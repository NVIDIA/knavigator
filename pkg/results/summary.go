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

// phaseDeleted marks a pod that was deleted, preempted or evicted before it finished.
const phaseDeleted = "Deleted"

// Pod is the lifecycle of one pod. Zero times mean the event never happened.
type Pod struct {
	Namespace string
	Name      string
	Job       string
	Node      string
	GPUs      float64
	// Priority is the pod's resolved priority (spec.priority), 0 when it has none.
	Priority  int
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
	Class     string // the class the job was submitted with, if any
}

// Stats holds durations in seconds.
type Stats struct {
	Count int     `json:"count"`
	Mean  float64 `json:"mean"`
	P95   float64 `json:"p95"`
	Max   float64 `json:"max"`
}

type Summary struct {
	Speedup         float64 `json:"speedup,omitempty"`
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
	// JobsByGangSize counts jobs by pods per job: submitted, waiting, running and complete.
	JobsByGangSize []GangSize `json:"jobs_by_gang_size"`
	// WaitByClass breaks job wait down by the class jobs were submitted with; empty without classes.
	WaitByClass []ClassWait `json:"wait_by_class,omitempty"`
	// PodsEvicted counts pods deleted after they were placed and before they finished; schedulers
	// preempt by deleting pods, and job controllers and cleanup do too.
	PodsEvicted int `json:"pods_evicted"`
	// JobsEvicted counts jobs that lost at least one pod that way.
	JobsEvicted int `json:"jobs_evicted"`
	// EvictedGPUHours is the GPU time those pods had already used, work that a restart repeats.
	EvictedGPUHours float64 `json:"evicted_gpu_hours"`
	// PreemptionEvents counts the scheduler's own preemption events, one per removed pod.
	PreemptionEvents int `json:"preemption_events"`
	// IdleWaitingGPUHours is GPU time that sat idle while pods were waiting for GPUs: at each moment
	// the smaller of the idle GPUs and the GPUs the waiting pods request, added up over the run.
	IdleWaitingGPUHours float64 `json:"idle_waiting_gpu_hours"`
	// BindingGapGPUHours is GPU time that gang pods held while waiting for the rest of their gang to be
	// bound, so the gang could not yet start.
	BindingGapGPUHours float64 `json:"binding_gap_gpu_hours"`
	// WastedGPUHours is EvictedGPUHours plus BindingGapGPUHours.
	WastedGPUHours float64 `json:"wasted_gpu_hours"`
	// EvictionsByPriority groups the evicted pods by the victim's priority, lowest first.
	EvictionsByPriority []PriorityEvictions `json:"evictions_by_priority"`
	// JobsEvictedAgain counts jobs that lost more pods than they have, so a pod was evicted at least
	// twice. Each restart repeats the lost work.
	JobsEvictedAgain int `json:"jobs_evicted_again"`
	// MaxEvictionsPerJob is the most times one job was evicted; a whole gang evicted at once counts once.
	MaxEvictionsPerJob int `json:"max_evictions_per_job"`
	// Stalled is set when the run stopped because nothing was placed or finished for -results.stall.
	Stalled bool `json:"stalled"`
	Sizes
	Fragmentation
}

// PriorityEvictions counts the evicted pods of one priority.
type PriorityEvictions struct {
	Priority int     `json:"priority"`
	Pods     int     `json:"pods"`
	Jobs     int     `json:"jobs"`
	GPUHours float64 `json:"gpu_hours"`
}

// Summarize sets Pod.Job and computes run metrics. Pods still running at end count as running until end.
func Summarize(pods []Pod, jobs []Job, gpuCapacity float64, end time.Time, opt Options) Summary {
	assignJobs(pods, jobs)

	s := Summary{Pods: len(pods), Jobs: len(jobs), GPUCapacity: gpuCapacity}
	var first, last time.Time
	var podWaits []float64
	// Earliest placement per pod name, so a replacement attempt cannot fill another worker's slot.
	scheduled := map[string]map[string]time.Time{}
	evicted := map[string]bool{}
	evictedPods := map[string]int{}
	byPriority := map[int]*PriorityEvictions{}
	priorityJobs := map[int]map[string]bool{}

	for _, p := range pods {
		first = earliest(first, p.Created)
		if p.Phase == string(corev1.PodFailed) {
			s.PodsFailed++
		}
		if p.Scheduled.IsZero() {
			s.PodsUnscheduled++
			continue
		}
		fin := p.Finished
		if fin.IsZero() {
			fin = end
		}
		last = latest(last, fin)
		s.GPUHours += p.GPUs * fin.Sub(p.Scheduled).Hours()
		if p.Phase == phaseDeleted {
			s.PodsEvicted++
			s.EvictedGPUHours += p.GPUs * fin.Sub(p.Scheduled).Hours()
			evicted[p.Job] = true
			evictedPods[p.Job]++
			e := byPriority[p.Priority]
			if e == nil {
				e = &PriorityEvictions{Priority: p.Priority}
				byPriority[p.Priority] = e
				priorityJobs[p.Priority] = map[string]bool{}
			}
			e.Pods++
			e.GPUHours += p.GPUs * fin.Sub(p.Scheduled).Hours()
			priorityJobs[p.Priority][p.Job] = true
		}
		podWaits = append(podWaits, p.Scheduled.Sub(p.Created).Seconds())
		if scheduled[p.Job] == nil {
			scheduled[p.Job] = map[string]time.Time{}
		}
		if t, ok := scheduled[p.Job][p.Name]; !ok || p.Scheduled.Before(t) {
			scheduled[p.Job][p.Name] = p.Scheduled
		}
	}

	delete(evicted, "") // pods that belong to no submitted job
	s.JobsEvicted = len(evicted)
	s.IdleWaitingGPUHours = idleWaitingGPUHours(pods, gpuCapacity, end)
	s.BindingGapGPUHours = bindingGapGPUHours(pods, jobs, end)
	s.WastedGPUHours = s.EvictedGPUHours + s.BindingGapGPUHours
	for _, pe := range byPriority {
		pe.Jobs = len(priorityJobs[pe.Priority])
		s.EvictionsByPriority = append(s.EvictionsByPriority, *pe)
	}
	sort.Slice(s.EvictionsByPriority, func(a, b int) bool { return s.EvictionsByPriority[a].Priority < s.EvictionsByPriority[b].Priority })
	var jobWaits []float64
	waitOf := map[string]float64{}
	for _, j := range jobs {
		first = earliest(first, j.Submitted)
		if j.Pods == 0 {
			continue
		}
		if len(scheduled[j.Name]) < j.Pods {
			continue
		}
		ts := make([]time.Time, 0, len(scheduled[j.Name]))
		for _, t := range scheduled[j.Name] {
			ts = append(ts, t)
		}
		// A job starts when its last required pod is placed, so a gang waits for its slowest member.
		sort.Slice(ts, func(a, b int) bool { return ts[a].Before(ts[b]) })
		s.JobsStarted++
		// Pod timestamps have one second resolution, so a fast placement can read as before submission.
		waitOf[j.Name] = math.Max(0, ts[j.Pods-1].Sub(j.Submitted).Seconds())
		jobWaits = append(jobWaits, waitOf[j.Name])
	}

	for _, j := range jobs {
		if e := evictedPods[j.Name]; j.Pods > 0 && e > 0 {
			if e > j.Pods {
				s.JobsEvictedAgain++
			}
			s.MaxEvictionsPerJob = max(s.MaxEvictionsPerJob, (e+j.Pods-1)/j.Pods)
		}
	}

	// Work still waiting at the end, including replacements for evicted pods, keeps the cluster's
	// idle time in the denominator.
	if !idle(pods, jobs) {
		last = latest(last, end)
	}
	if !first.IsZero() && last.After(first) {
		s.MakespanSec = last.Sub(first).Seconds()
		if gpuCapacity > 0 {
			s.GPUUtilization = s.GPUHours / (gpuCapacity * s.MakespanSec / 3600)
		}
		s.Fragmentation = fragmentation(pods, opt.NodeGPUs, first, last)
	}
	s.PodWaitSec = stats(podWaits)
	s.JobWaitSec = stats(jobWaits)
	s.JobsByGangSize = byGangSize(pods, jobs, scheduled)
	s.WaitByClass = byClass(jobs, waitOf, opt.Speedup)
	s.Sizes = sizeMetrics(pods, jobs, scheduled, end, opt)
	return s
}

// ScaleTime converts the durations from real seconds to simulated seconds for a run that
// compressed time by speedup. Ratios such as GPU utilization are unchanged.
func (s *Summary) ScaleTime(speedup float64) {
	s.Speedup = speedup
	s.MakespanSec *= speedup
	s.GPUHours *= speedup
	s.EvictedGPUHours *= speedup
	s.IdleWaitingGPUHours *= speedup
	s.BindingGapGPUHours *= speedup
	s.WastedGPUHours *= speedup
	s.FragmentedGPUHours *= speedup
	for i := range s.EvictionsByPriority {
		s.EvictionsByPriority[i].GPUHours *= speedup
	}
	for _, st := range []*Stats{&s.PodWaitSec, &s.JobWaitSec} {
		st.Mean *= speedup
		st.P95 *= speedup
		st.Max *= speedup
	}
	s.AbandonedGPUHours *= speedup
	for i := range s.WaitByFootprint {
		w := &s.WaitByFootprint[i].WaitSec
		w.Mean *= speedup
		w.P95 *= speedup
		w.Max *= speedup
	}
	for i := range s.JobsByGangSize {
		w := &s.JobsByGangSize[i].WaitSec
		w.Mean *= speedup
		w.P95 *= speedup
		w.Max *= speedup
	}
	for i := range s.WaitByClass {
		w := &s.WaitByClass[i].WaitSec
		w.Mean *= speedup
		w.P95 *= speedup
		w.Max *= speedup
	}
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
	case s.Stalled:
		return fmt.Errorf("run stalled: %d/%d jobs started", s.JobsStarted, s.Jobs)
	case !drained:
		return fmt.Errorf("run did not drain: %d/%d jobs started", s.JobsStarted, s.Jobs)
	case s.PodsFailed > 0:
		return fmt.Errorf("%d pods failed", s.PodsFailed)
	case s.JobsStarted < s.Jobs:
		return fmt.Errorf("only %d/%d jobs started", s.JobsStarted, s.Jobs)
	}
	return nil
}
