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

// Package results records pod lifecycles during a run and computes scheduling metrics.
package results

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Pod is the lifecycle of one pod. Zero times mean the event never happened.
type Pod struct {
	Namespace string
	Name      string
	Job       string
	Node      string
	GPUs      float64
	Phase     string
	Created   time.Time
	Scheduled time.Time
	Finished  time.Time
}

// Job is one object submitted by a SubmitObj task.
type Job struct {
	Name      string // namespace/name
	Namespace string
	Submitted time.Time
	Pods      int    // pods the object should create
	PodRegexp string // matches the full pod name
}

// Stats holds durations in seconds.
type Stats struct {
	Count int     `json:"count"`
	Mean  float64 `json:"mean"`
	P95   float64 `json:"p95"`
	Max   float64 `json:"max"`
}

type Summary struct {
	Pods            int     `json:"pods"`
	PodsUnscheduled int     `json:"pods_unscheduled"`
	PodsFailed      int     `json:"pods_failed"`
	Jobs            int     `json:"jobs"`
	JobsStarted     int     `json:"jobs_started"`
	MakespanSec     float64 `json:"makespan_sec"`
	GPUCapacity     float64 `json:"gpu_capacity"`
	GPUHours        float64 `json:"gpu_hours"`
	GPUUtilization  float64 `json:"gpu_utilization"`
	PodWaitSec      Stats   `json:"pod_wait_sec"`
	JobWaitSec      Stats   `json:"job_wait_sec"`
}

// Summarize sets Pod.Job and computes run metrics. Pods still running at end count as running until end.
func Summarize(pods []Pod, jobs []Job, gpuCapacity float64, end time.Time) Summary {
	assignJobs(pods, jobs)

	s := Summary{Pods: len(pods), Jobs: len(jobs), GPUCapacity: gpuCapacity}
	var first, last time.Time
	var podWaits []float64
	// Earliest placement per pod name, so a replacement attempt cannot fill another worker's slot.
	scheduled := map[string]map[string]time.Time{}
	outstanding := false

	for _, p := range pods {
		first = earliest(first, p.Created)
		if p.Phase == string(corev1.PodFailed) {
			s.PodsFailed++
		}
		if p.Scheduled.IsZero() {
			s.PodsUnscheduled++
			outstanding = outstanding || p.Finished.IsZero()
			continue
		}
		fin := p.Finished
		if fin.IsZero() {
			fin = end
		}
		last = latest(last, fin)
		s.GPUHours += p.GPUs * fin.Sub(p.Scheduled).Hours()
		podWaits = append(podWaits, p.Scheduled.Sub(p.Created).Seconds())
		if scheduled[p.Job] == nil {
			scheduled[p.Job] = map[string]time.Time{}
		}
		if t, ok := scheduled[p.Job][p.Name]; !ok || p.Scheduled.Before(t) {
			scheduled[p.Job][p.Name] = p.Scheduled
		}
	}

	var jobWaits []float64
	for _, j := range jobs {
		first = earliest(first, j.Submitted)
		if j.Pods == 0 {
			continue
		}
		if len(scheduled[j.Name]) < j.Pods {
			outstanding = true
			continue
		}
		ts := make([]time.Time, 0, len(scheduled[j.Name]))
		for _, t := range scheduled[j.Name] {
			ts = append(ts, t)
		}
		// A job starts when its last required pod is placed, so a gang waits for its slowest member.
		sort.Slice(ts, func(a, b int) bool { return ts[a].Before(ts[b]) })
		s.JobsStarted++
		jobWaits = append(jobWaits, ts[j.Pods-1].Sub(j.Submitted).Seconds())
	}

	// Work still waiting at the end keeps the cluster's idle time in the denominator.
	if outstanding {
		last = latest(last, end)
	}
	if !first.IsZero() && last.After(first) {
		s.MakespanSec = last.Sub(first).Seconds()
		if gpuCapacity > 0 {
			s.GPUUtilization = s.GPUHours / (gpuCapacity * s.MakespanSec / 3600)
		}
	}
	s.PodWaitSec = stats(podWaits)
	s.JobWaitSec = stats(jobWaits)
	return s
}

func assignJobs(pods []Pod, jobs []Job) {
	res := make([]*regexp.Regexp, len(jobs))
	for i, j := range jobs {
		if j.PodRegexp != "" {
			// A bad pattern leaves the job unmatched; CheckPod tasks report the compile error.
			res[i], _ = regexp.Compile("^(?:" + j.PodRegexp + ")$")
		}
	}
	for i := range pods {
		for k, re := range res {
			if re != nil && pods[i].Namespace == jobs[k].Namespace && re.MatchString(pods[i].Name) {
				pods[i].Job = jobs[k].Name
				break
			}
		}
	}
}

// stats uses nearest-rank percentiles.
func stats(v []float64) Stats {
	if len(v) == 0 {
		return Stats{}
	}
	sort.Float64s(v)
	var sum float64
	for _, x := range v {
		sum += x
	}
	rank := int(math.Ceil(0.95*float64(len(v)))) - 1
	return Stats{Count: len(v), Mean: sum / float64(len(v)), P95: v[rank], Max: v[len(v)-1]}
}

func earliest(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// check fails a run that left work unfinished, so a benchmark gate can rely on the exit code.
func (s Summary) check(drained bool) error {
	switch {
	case !drained:
		return fmt.Errorf("run did not drain: %d/%d jobs started", s.JobsStarted, s.Jobs)
	case s.PodsFailed > 0:
		return fmt.Errorf("%d pods failed", s.PodsFailed)
	case s.JobsStarted < s.Jobs:
		return fmt.Errorf("only %d/%d jobs started", s.JobsStarted, s.Jobs)
	}
	return nil
}
