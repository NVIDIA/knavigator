/*
 * Copyright (c) 2026, NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package verdict

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// FairShareParams are the fair-share probe's parameters.
type FairShareParams struct {
	TenantB         string  `json:"tenant_b"`
	EntitlementGPUs int64   `json:"entitlement_gpus"`
	JobLengthSec    float64 `json:"job_length_sec"`
	ToleranceSec    float64 `json:"tolerance_sec"`
	CapacityGPUs    int64   `json:"capacity_gpus"`
	WindowSec       float64 `json:"window_sec"`
}

func (p FairShareParams) validate() error {
	if p.TenantB == "" {
		return errors.New("tenant_b is empty")
	}
	if p.EntitlementGPUs < 1 || p.CapacityGPUs < 1 {
		return errors.New("entitlement_gpus and capacity_gpus must be 1 or more")
	}
	if p.JobLengthSec <= 0 || p.WindowSec <= 0 || p.ToleranceSec < 0 {
		return errors.New("job_length_sec and window_sec must be positive, tolerance_sec not negative")
	}
	return nil
}

// FairShare judges whether tenant B received its entitlement within one
// job length (plus tolerance) of its first submission: the most GPUs B had
// running at any moment of [submission, submission + length + tolerance].
// It also records how long after the probe's first submission B arrived,
// and the fairness of capacity over time across the probe's tenants.
func FairShare(t Timeline, p FairShareParams) (Verdict, error) {
	if err := p.validate(); err != nil {
		return Verdict{}, err
	}
	var bSubmit time.Time
	for _, j := range t.Jobs {
		if j.Tenant == p.TenantB && !j.Created.IsZero() && (bSubmit.IsZero() || j.Created.Before(bSubmit)) {
			bSubmit = j.Created
		}
	}
	if bSubmit.IsZero() {
		return Verdict{}, fmt.Errorf("no submission recorded for tenant %s", p.TenantB)
	}
	first := bSubmit
	for _, j := range t.Jobs {
		if !j.Created.IsZero() && j.Created.Before(first) {
			first = j.Created
		}
	}
	var bPods []*Pod
	for _, q := range t.Pods {
		if q.Tenant == p.TenantB {
			bPods = append(bPods, q)
		}
	}
	offset := p.JobLengthSec + p.ToleranceSec
	check := bSubmit.Add(seconds(offset))
	bMax := maxRunningGPUs(bPods, check)
	f := fairnessOverTime(t, p.CapacityGPUs, p.WindowSec)
	v := Verdict{
		Measurements: map[string]float64{
			"b_max_gpus_by_check":    float64(bMax),
			"check_offset_s":         offset,
			"b_after_first_submit_s": bSubmit.Sub(first).Seconds(),
			"windows_used":           float64(f.Used),
			"windows_excluded":       float64(f.Excluded),
			"worst_shortfall_gpu_s":  f.WorstShortfall,
		},
		EvidenceSeq: evidence(t.Pods, t.jobSeqs(jobNames(t)...)),
	}
	if f.Used > 0 {
		v.Measurements["jain_median"] = f.Median
		v.Measurements["jain_p5"] = f.P5
	}
	switch {
	case bMax >= p.EntitlementGPUs:
		v.Outcome = Supported
		v.Reason = fmt.Sprintf("tenant %s had %d GPUs running within %s s of submitting; its entitlement is %d", p.TenantB, bMax, fmtSec(offset), p.EntitlementGPUs)
	case bMax > 0:
		v.Outcome = Partial
		v.Reason = fmt.Sprintf("tenant %s had at most %d of its %d entitled GPUs running within %s s of submitting", p.TenantB, bMax, p.EntitlementGPUs, fmtSec(offset))
	default:
		v.Outcome = NotSupported
		v.Reason = fmt.Sprintf("tenant %s had no GPUs running within %s s of submitting", p.TenantB, fmtSec(offset))
	}
	return v, nil
}

// maxRunningGPUs is the most GPUs the pods had running at one moment, over
// every moment up to and including check. A pod runs over [Running, End).
func maxRunningGPUs(pods []*Pod, check time.Time) int64 {
	type edge struct {
		at    time.Time
		delta int64
	}
	var edges []edge
	for _, q := range pods {
		if q.Running.IsZero() || q.Running.After(check) {
			continue
		}
		edges = append(edges, edge{q.Running, q.GPUs})
		if !q.End.IsZero() {
			edges = append(edges, edge{q.End, -q.GPUs})
		}
	}
	// at equal times, apply ends before starts: a pod ending as another
	// starts does not count both
	sort.Slice(edges, func(i, j int) bool {
		if !edges[i].at.Equal(edges[j].at) {
			return edges[i].at.Before(edges[j].at)
		}
		return edges[i].delta < edges[j].delta
	})
	var cur, best int64
	for _, e := range edges {
		cur += e.delta
		if cur > best {
			best = cur
		}
	}
	return best
}

func jobNames(t Timeline) []string {
	out := make([]string, 0, len(t.Jobs))
	for n := range t.Jobs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// fairness summarizes fairness over time: Jain's index of delivered over
// entitled GPU-time, per window.
type fairness struct {
	Median, P5     float64
	Used, Excluded int
	WorstShortfall float64
}

// fairnessOverTime splits [first submission, window end) into windows of
// windowSec (the last may be shorter). Per window and tenant: demand is the
// GPU-seconds a job wanted from its submission until it ended (so a job a
// queue holds without pods still counts), delivered is the GPU-seconds its
// pods ran, and entitlement is the max-min fair split of capacity capped by
// demand. Jain's index of delivered/entitlement uses windows with at least
// two tenants with demand and some delivery; the shortfall sums
// max(0, entitlement - delivered) over every window with demand.
func fairnessOverTime(t Timeline, capacityGPUs int64, windowSec float64) fairness {
	var start time.Time
	for _, j := range t.Jobs {
		if !j.Created.IsZero() && (start.IsZero() || j.Created.Before(start)) {
			start = j.Created
		}
	}
	if start.IsZero() {
		return fairness{}
	}
	horizon := t.End.Sub(start).Seconds()
	rel := func(x time.Time, dflt float64) float64 {
		if x.IsZero() {
			return dflt
		}
		return x.Sub(start).Seconds()
	}
	type span struct {
		tenant   string
		gpus     float64
		from, to float64
	}
	var want, ran []span
	for _, n := range jobNames(t) {
		j := t.Jobs[n]
		if !j.Created.IsZero() {
			want = append(want, span{j.Tenant, float64(j.GPUs), rel(j.Created, 0), rel(j.End, horizon)})
		}
	}
	for _, q := range t.Pods {
		if !q.Running.IsZero() {
			ran = append(ran, span{q.Tenant, float64(q.GPUs), rel(q.Running, 0), rel(q.End, horizon)})
		}
	}
	var js []float64
	f := fairness{}
	shortfall := map[string]float64{}
	for k := 0; float64(k)*windowSec < horizon; k++ {
		// every product that reaches an addition or subtraction is rounded
		// by an explicit float64 conversion: Go may otherwise fuse it into
		// a multiply-add (arm64 does, amd64 does not), and a run and its
		// rebuild on another architecture would differ in the last bit
		w0 := float64(float64(k) * windowSec)
		w1 := math.Min(w0+windowSec, horizon)
		demand, delivered := map[string]float64{}, map[string]float64{}
		for _, s := range want {
			if d := float64(s.gpus * overlap(s.from, s.to, w0, w1)); d > 0 {
				demand[s.tenant] += d
			}
		}
		for _, s := range ran {
			delivered[s.tenant] += float64(s.gpus * overlap(s.from, s.to, w0, w1))
		}
		if len(demand) == 0 {
			f.Excluded++
			continue
		}
		ent := waterFill(float64(float64(capacityGPUs)*(w1-w0)), demand)
		for tn, e := range ent {
			shortfall[tn] += math.Max(0, e-delivered[tn])
		}
		if len(demand) < 2 {
			f.Excluded++
			continue
		}
		// sum in tenant name order: float addition order would otherwise
		// follow map order and change the last bit between a run and its rebuild
		names := make([]string, 0, len(ent))
		for tn := range ent {
			names = append(names, tn)
		}
		sort.Strings(names)
		var sum, sq float64
		n := 0
		for _, tn := range names {
			if e := ent[tn]; e > 0 {
				x := delivered[tn] / e
				sum += x
				sq += float64(x * x)
				n++
			}
		}
		if sq == 0 {
			f.Excluded++
			continue
		}
		js = append(js, sum*sum/(float64(n)*sq))
	}
	for _, s := range shortfall {
		f.WorstShortfall = math.Max(f.WorstShortfall, s)
	}
	f.Used = len(js)
	if f.Used == 0 {
		return f
	}
	sort.Float64s(js)
	if f.Used%2 == 1 {
		f.Median = js[f.Used/2]
	} else {
		f.Median = (js[f.Used/2-1] + js[f.Used/2]) / 2
	}
	f.P5 = js[int(math.Ceil(0.05*float64(f.Used)))-1]
	return f
}

// overlap returns the length of [a, b) intersected with [w0, w1).
func overlap(a, b, w0, w1 float64) float64 {
	return math.Max(0, math.Min(b, w1)-math.Max(a, w0))
}

// waterFill splits capacity max-min fairly among the tenants in demand,
// never giving a tenant more than its demand.
func waterFill(capacity float64, demand map[string]float64) map[string]float64 {
	names := make([]string, 0, len(demand))
	for t := range demand {
		names = append(names, t)
	}
	sort.Slice(names, func(i, j int) bool {
		if demand[names[i]] != demand[names[j]] {
			return demand[names[i]] < demand[names[j]]
		}
		return names[i] < names[j]
	})
	out := map[string]float64{}
	remaining := capacity
	for i, t := range names {
		give := math.Min(demand[t], remaining/float64(len(names)-i))
		out[t] = give
		remaining -= give
	}
	return out
}
