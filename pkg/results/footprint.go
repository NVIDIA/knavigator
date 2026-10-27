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

package results

import (
	"math"
	"sort"
	"time"
)

// Options tune metrics that depend on the cluster or on the run's time scale.
type Options struct {
	// MaxNodeGPUs is the largest GPU count on one node; jobs at least this big are "large".
	MaxNodeGPUs float64
	// BSLDFloor bounds the runtime in bounded slowdown. Scale it with -speedup.
	BSLDFloor time.Duration
	// Speedup converts real waits to simulated time for the SLO limits; 0 means 1.
	Speedup float64
	// NodeGPUs maps each node to its allocatable GPUs, for fragmentation.
	NodeGPUs map[string]float64
	// NodeRacks maps GPU nodes to their racks; empty leaves out rack fragmentation.
	NodeRacks map[string]string
}

// DefaultBSLDFloor is the runtime below which slowdown reports queueing noise rather than service.
const DefaultBSLDFloor = 10 * time.Minute

type bucket struct {
	label    string
	low, top float64 // top 0 means open-ended
}

// Job footprint is total GPUs: GPUs per pod times pods.
var buckets = []bucket{{"1", 1, 1}, {"2", 2, 2}, {"3-4", 3, 4}, {"5-8", 5, 8}, {"9-16", 9, 16}, {"17+", 17, 0}}

// Footprint summarizes jobs in one footprint bucket.
type Footprint struct {
	Bucket  string `json:"bucket"`
	Jobs    int    `json:"jobs"`
	Started int    `json:"started"`
	WaitSec Stats  `json:"wait_sec"`
	// BSLD is bounded slowdown, max(1, (wait + runtime) / max(runtime, floor)), over finished jobs.
	BSLD Stats `json:"bsld"`
}

// Sizes holds the metrics that show whether a scheduler favors some job sizes.
type Sizes struct {
	// FootprintWaitSpearman is the rank correlation of job size and wait; null when undefined.
	FootprintWaitSpearman *float64 `json:"footprint_wait_spearman"`
	// LargeJobStarvationRatio is mean wait of jobs of at least one node over mean wait of 1-GPU jobs.
	LargeJobStarvationRatio *float64    `json:"large_job_starvation_ratio"`
	WaitByFootprint         []Footprint `json:"wait_by_footprint"`
	// BSLDMacro averages the per-bucket mean slowdown with equal weight per bucket. Null unless
	// every job finished, because slowdown is unbounded for work that never ran.
	BSLDMacro *float64 `json:"bsld_macro"`
	// JobsNeverAdmitted counts jobs with a pod that was never placed.
	JobsNeverAdmitted int `json:"jobs_never_admitted"`
	// JobsAbandoned counts jobs fully placed but still running when the run ended.
	JobsAbandoned     int     `json:"jobs_abandoned"`
	AbandonedGPUHours float64 `json:"abandoned_gpu_hours"`
}

// sizeMetrics assumes Pod.Job is set. start maps job name to the earliest placement per pod name.
func sizeMetrics(pods []Pod, jobs []Job, start map[string]map[string]time.Time, end time.Time, opt Options) Sizes {
	byJob := map[string][]Pod{}
	for _, p := range pods {
		byJob[p.Job] = append(byJob[p.Job], p)
	}
	floor := opt.BSLDFloor.Seconds()

	var sz Sizes
	waits := make([][]float64, len(buckets))
	bslds := make([][]float64, len(buckets))
	counts := make([]Footprint, len(buckets))
	var sizesX, waitsY, large, single []float64

	for _, j := range jobs {
		if j.Pods == 0 {
			continue
		}
		jp := byJob[j.Name]
		placed := start[j.Name]
		footprint := jobGPUs(jp)
		bi := bucketOf(footprint)
		if bi >= 0 {
			counts[bi].Jobs++
		}
		if len(placed) < j.Pods {
			sz.JobsNeverAdmitted++
			continue
		}

		ts := make([]time.Time, 0, len(placed))
		for _, t := range placed {
			ts = append(ts, t)
		}
		sort.Slice(ts, func(a, b int) bool { return ts[a].Before(ts[b]) })
		assembled := ts[j.Pods-1]
		// Pod timestamps have one second resolution, so a fast placement can read as before submission.
		wait := math.Max(0, assembled.Sub(j.Submitted).Seconds())

		if bi >= 0 {
			counts[bi].Started++
			waits[bi] = append(waits[bi], wait)
		}
		sizesX = append(sizesX, footprint)
		waitsY = append(waitsY, wait)
		if opt.MaxNodeGPUs > 0 && footprint >= opt.MaxNodeGPUs {
			large = append(large, wait)
		}
		if footprint == 1 {
			single = append(single, wait)
		}

		fin, ok := finished(jp, j.Pods)
		if !ok {
			sz.JobsAbandoned++
			sz.AbandonedGPUHours += heldGPUHours(jp, end)
			continue
		}
		if bi >= 0 {
			run := fin.Sub(assembled).Seconds()
			bslds[bi] = append(bslds[bi], math.Max(1, (wait+run)/math.Max(run, floor)))
		}
	}

	var means []float64
	for i, b := range buckets {
		counts[i].Bucket = b.label
		counts[i].WaitSec = stats(waits[i])
		counts[i].BSLD = stats(bslds[i])
		if len(bslds[i]) > 0 {
			means = append(means, counts[i].BSLD.Mean)
		}
	}
	sz.WaitByFootprint = counts
	if sz.JobsNeverAdmitted == 0 && sz.JobsAbandoned == 0 && len(means) > 0 {
		m := mean(means)
		sz.BSLDMacro = &m
	}
	sz.FootprintWaitSpearman = spearman(sizesX, waitsY)
	if len(large) > 0 && len(single) > 0 && mean(single) > 0 {
		r := mean(large) / mean(single)
		sz.LargeJobStarvationRatio = &r
	}
	return sz
}

// jobGPUs sums GPUs over distinct pod names.
func jobGPUs(pods []Pod) float64 {
	gpus := map[string]float64{}
	for _, p := range pods {
		gpus[p.Name] = math.Max(gpus[p.Name], p.GPUs)
	}
	var n float64
	for _, g := range gpus {
		n += g
	}
	return n
}

func bucketOf(gpus float64) int {
	for i, b := range buckets {
		if gpus >= b.low && (b.top == 0 || gpus <= b.top) {
			return i
		}
	}
	return -1
}

func heldGPUHours(pods []Pod, end time.Time) float64 {
	var h float64
	for _, p := range pods {
		if p.Scheduled.IsZero() {
			continue
		}
		fin := p.Finished
		if fin.IsZero() {
			fin = end
		}
		h += p.GPUs * fin.Sub(p.Scheduled).Hours()
	}
	return h
}

// finished reports when a job ended. Its final attempt of every pod name must have finished.
func finished(pods []Pod, want int) (time.Time, bool) {
	last := map[string]Pod{}
	for _, p := range pods {
		if q, ok := last[p.Name]; !ok || p.Created.After(q.Created) ||
			(p.Created.Equal(q.Created) && p.Scheduled.After(q.Scheduled)) {
			last[p.Name] = p
		}
	}
	var fin time.Time
	for _, p := range last {
		if p.Scheduled.IsZero() || p.Finished.IsZero() {
			return time.Time{}, false
		}
		fin = latest(fin, p.Finished)
	}
	return fin, len(last) >= want
}

func mean(v []float64) float64 {
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// spearman is the rank correlation with tied values sharing their average rank; nil if undefined.
func spearman(x, y []float64) *float64 {
	if len(x) < 2 {
		return nil
	}
	rx, ry := ranks(x), ranks(y)
	mx, my := mean(rx), mean(ry)
	var sxy, sxx, syy float64
	for i := range rx {
		sxy += (rx[i] - mx) * (ry[i] - my)
		sxx += (rx[i] - mx) * (rx[i] - mx)
		syy += (ry[i] - my) * (ry[i] - my)
	}
	if sxx == 0 || syy == 0 {
		return nil
	}
	r := sxy / math.Sqrt(sxx*syy)
	return &r
}

func ranks(v []float64) []float64 {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	r := make([]float64, len(v))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && v[idx[j+1]] == v[idx[i]] {
			j++
		}
		for k := i; k <= j; k++ {
			r[idx[k]] = float64(i+j)/2 + 1
		}
		i = j + 1
	}
	return r
}
