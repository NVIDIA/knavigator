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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSummarize(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

	jobs := []Job{
		{Name: "default/j1", Namespace: "default", Submitted: t0, Pods: 2, PodRegexp: "j1-test-[0-9]+"},
		{Name: "default/j10", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "j10-test-[0-9]+"},
	}
	pods := []Pod{
		{Namespace: "default", Name: "j1-test-0", GPUs: 8, Created: at(1), Scheduled: at(5), Finished: at(70)},
		{Namespace: "default", Name: "j1-test-1", GPUs: 8, Created: at(1), Scheduled: at(10), Finished: at(70)},
		{Namespace: "default", Name: "j10-test-0", GPUs: 8, Created: at(1)},
		// No owning job and still running at the end of the run.
		{Namespace: "other", Name: "x", GPUs: 4, Created: at(2), Scheduled: at(2)},
	}

	s := Summarize(pods, jobs, 16, at(100), Options{MaxNodeGPUs: 8, BSLDFloor: time.Minute})

	require.Equal(t, []string{"default/j1", "default/j1", "default/j10", ""},
		[]string{pods[0].Job, pods[1].Job, pods[2].Job, pods[3].Job})
	require.Equal(t, 4, s.Pods)
	require.Equal(t, 1, s.PodsUnscheduled)
	require.Equal(t, 2, s.Jobs)
	require.Equal(t, 1, s.JobsStarted)
	require.Equal(t, 100.0, s.MakespanSec)
	// 8*65 + 8*60 + 4*98 GPU-seconds over 16 GPUs for 100 seconds.
	require.InDelta(t, 1392.0/3600, s.GPUHours, 1e-9)
	require.InDelta(t, 0.87, s.GPUUtilization, 1e-9)
	require.Equal(t, Stats{Count: 3, Mean: 13.0 / 3, P95: 9, Max: 9}, s.PodWaitSec)
	// The gang starts when its second pod lands.
	require.Equal(t, Stats{Count: 1, Mean: 10, P95: 10, Max: 10}, s.JobWaitSec)
}

func TestSummarizeOutstanding(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

	jobs := []Job{
		{Name: "default/a", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "a-[0-9]+"},
		{Name: "default/g", Namespace: "default", Submitted: t0, Pods: 2, PodRegexp: "g-[0-9]+"},
		{Name: "default/n", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "n-[0-9]+"},
	}
	pods := []Pod{
		{Namespace: "default", Name: "a-0", GPUs: 8, Created: at(0), Scheduled: at(0), Finished: at(10)},
		// Worker 0 is placed, fails and is replaced; worker 1 never lands.
		{Namespace: "default", Name: "g-0", Created: at(0), Scheduled: at(1), Finished: at(2)},
		{Namespace: "default", Name: "g-0", Created: at(2), Scheduled: at(3), Finished: at(4)},
		{Namespace: "default", Name: "g-1", Created: at(0)},
	}

	// The unplaced pods and the job with no pods keep the run open until the timeout.
	s := Summarize(pods, jobs, 8, at(3600), Options{})
	require.Equal(t, 3600.0, s.MakespanSec)
	require.Equal(t, 1, s.JobsStarted)

	// A drained run ends at its last completion.
	s = Summarize(pods[:1], jobs[:1], 8, at(3600), Options{})
	require.Equal(t, 10.0, s.MakespanSec)
	require.InDelta(t, 1.0, s.GPUUtilization, 1e-9)

	// An evicted pod whose replacement does not exist yet keeps the run open too.
	evicted := []Pod{{Namespace: "default", Name: "a-0", GPUs: 8, Phase: phaseDeleted, Created: at(0), Scheduled: at(0), Finished: at(10)}}
	s = Summarize(evicted, jobs[:1], 8, at(100), Options{})
	require.Equal(t, 100.0, s.MakespanSec)
	require.InDelta(t, 0.1, s.GPUUtilization, 1e-9)
}

func TestCheck(t *testing.T) {
	require.NoError(t, Summary{Jobs: 2, JobsStarted: 2}.check(true))
	require.EqualError(t, Summary{Jobs: 2, JobsStarted: 1, Stalled: true}.check(false), "run stalled: 1/2 jobs started")
	require.EqualError(t, Summary{Jobs: 2, JobsStarted: 1}.check(false), "run did not drain: 1/2 jobs started")
	require.EqualError(t, Summary{Jobs: 2, JobsStarted: 2, PodsFailed: 1}.check(true), "1 pods failed")
	require.EqualError(t, Summary{Jobs: 2, JobsStarted: 1}.check(true), "only 1/2 jobs started")
}

func TestScaleTime(t *testing.T) {
	s := Summary{MakespanSec: 10, GPUHours: 2, GPUUtilization: 0.5, JobWaitSec: Stats{Mean: 1, P95: 2, Max: 3}}
	s.ScaleTime(60)
	require.Equal(t, 60.0, s.Speedup)
	require.Equal(t, 600.0, s.MakespanSec)
	require.Equal(t, 120.0, s.GPUHours)
	require.Equal(t, 0.5, s.GPUUtilization)
	require.Equal(t, Stats{Mean: 60, P95: 120, Max: 180}, s.JobWaitSec)
}

func TestScaleTimeSizes(t *testing.T) {
	s := Summary{Sizes: Sizes{AbandonedGPUHours: 2,
		WaitByFootprint: []Footprint{{WaitSec: Stats{Mean: 1, P95: 2, Max: 3}, BSLD: Stats{Mean: 1.5}}}}}
	s.ScaleTime(10)
	require.Equal(t, 20.0, s.AbandonedGPUHours)
	require.Equal(t, Stats{Mean: 10, P95: 20, Max: 30}, s.WaitByFootprint[0].WaitSec)
	require.Equal(t, 1.5, s.WaitByFootprint[0].BSLD.Mean, "slowdown is a ratio")
}

func TestWaitByClass(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	job := func(name, class string) Job {
		return Job{Name: "default/" + name, Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: name + "-0", Class: class}
	}
	jobs := []Job{job("a", "inference"), job("b", "inference"), job("c", "training"), job("d", "")}
	pods := []Pod{
		{Namespace: "default", Name: "a-0", GPUs: 1, Created: t0, Scheduled: at(30), Finished: at(40)},
		// b never starts, so it misses every limit.
		{Namespace: "default", Name: "b-0", GPUs: 1, Created: t0},
		{Namespace: "default", Name: "c-0", GPUs: 8, Created: t0, Scheduled: at(100), Finished: at(200)},
		{Namespace: "default", Name: "d-0", GPUs: 1, Created: t0, Scheduled: at(1), Finished: at(2)},
	}

	// At 10x, a's 30s wait is 5 simulated minutes and c's 100s wait is about 17.
	s := Summarize(pods, jobs, 16, at(300), Options{Speedup: 10})

	require.Equal(t, []ClassWait{
		{Class: "inference", Jobs: 2, Started: 1, WaitSec: Stats{Count: 1, Mean: 30, P95: 30, Max: 30},
			SLOAttainment: map[string]float64{"1m": 0, "5m": 0.5, "15m": 0.5, "1h": 0.5, "4h": 0.5}},
		{Class: "training", Jobs: 1, Started: 1, WaitSec: Stats{Count: 1, Mean: 100, P95: 100, Max: 100},
			SLOAttainment: map[string]float64{"1m": 0, "5m": 0, "15m": 0, "1h": 1, "4h": 1}},
	}, s.WaitByClass, "jobs without a class are left out")

	s.ScaleTime(10)
	require.Equal(t, Stats{Count: 1, Mean: 300, P95: 300, Max: 300}, s.WaitByClass[0].WaitSec)
	require.Equal(t, 0.5, s.WaitByClass[0].SLOAttainment["5m"], "shares do not scale")

	require.Empty(t, Summarize(pods, jobs[3:], 16, at(300), Options{}).WaitByClass)
}

func TestByGangSize(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	job := func(n string, pods int) Job {
		return Job{Name: "default/" + n, Namespace: "default", Submitted: t0, Pods: pods, PodRegexp: n + "-[0-9]+"}
	}
	jobs := []Job{job("a", 1), job("b", 1), job("c", 1), job("g", 2), job("h", 2)}
	pods := []Pod{
		// a: done. b: running. c: not placed.
		{Namespace: "default", Name: "a-0", GPUs: 1, Phase: "Succeeded", Created: t0, Scheduled: at(2), Finished: at(10)},
		{Namespace: "default", Name: "b-0", GPUs: 1, Phase: "Running", Created: t0, Scheduled: at(4)},
		{Namespace: "default", Name: "c-0", GPUs: 1, Created: t0},
		// g: both pods done, one replaced after a preemption; its wait ends at the last pod's first
		// placement, as for Summary.JobWaitSec. h: one pod placed, so still waiting.
		{Namespace: "default", Name: "g-0", GPUs: 8, Created: t0, Scheduled: at(1), Finished: at(5), Phase: phaseDeleted},
		{Namespace: "default", Name: "g-0", GPUs: 8, Phase: "Succeeded", Created: at(5), Scheduled: at(6), Finished: at(20)},
		{Namespace: "default", Name: "g-1", GPUs: 8, Phase: "Succeeded", Created: t0, Scheduled: at(3), Finished: at(20)},
		{Namespace: "default", Name: "h-0", GPUs: 8, Created: t0, Scheduled: at(1)},
		{Namespace: "default", Name: "h-1", GPUs: 8, Created: t0},
	}

	s := Summarize(pods, jobs, 16, at(30), Options{})

	require.Equal(t, []GangSize{
		{Pods: 1, Submitted: 3, Waiting: 1, Running: 1, Complete: 1,
			WaitSec: Stats{Count: 2, Mean: 3, P95: 4, Max: 4}},
		{Pods: 2, Submitted: 2, Waiting: 1, Running: 0, Complete: 1,
			WaitSec: Stats{Count: 1, Mean: 3, P95: 3, Max: 3}},
	}, s.JobsByGangSize)

	s.ScaleTime(10)
	require.Equal(t, Stats{Count: 2, Mean: 30, P95: 40, Max: 40}, s.JobsByGangSize[0].WaitSec)
}

func TestByGangSizeSubSecondPlacement(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jobs := []Job{{Name: "default/a", Namespace: "default", Submitted: t0.Add(500 * time.Millisecond), Pods: 1, PodRegexp: "a-0"}}
	pods := []Pod{{Namespace: "default", Name: "a-0", GPUs: 1, Created: t0, Scheduled: t0, Finished: t0.Add(time.Second)}}

	s := Summarize(pods, jobs, 8, t0.Add(time.Minute), Options{})

	require.Equal(t, Stats{Count: 1}, s.JobWaitSec, "wait is never negative")
	require.Equal(t, Stats{Count: 1}, s.JobsByGangSize[0].WaitSec, "wait is never negative")
	require.Equal(t, Stats{Count: 1}, s.WaitByFootprint[0].WaitSec, "wait is never negative")
}

func TestByGangSizeReplacedPods(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	jobs := []Job{
		{Name: "default/a", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "a-[a-z0-9]+"},
		{Name: "default/b", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "b-[a-z0-9]+"},
	}
	pods := []Pod{
		// Job a: a pod that failed before it was placed, an evicted pod, then the pod that finished.
		{Namespace: "default", Name: "a-1", GPUs: 4, Phase: "Failed", Created: at(0), Finished: at(2)},
		{Namespace: "default", Name: "a-2", GPUs: 4, Phase: phaseDeleted, Created: at(2), Scheduled: at(3), Finished: at(9)},
		{Namespace: "default", Name: "a-3", GPUs: 4, Phase: "Succeeded", Created: at(9), Scheduled: at(10), Finished: at(60)},
		// Job b: evicted, with its replacement still running.
		{Namespace: "default", Name: "b-1", GPUs: 4, Phase: phaseDeleted, Created: at(0), Scheduled: at(1), Finished: at(8)},
		{Namespace: "default", Name: "b-2", GPUs: 4, Phase: "Running", Created: at(8), Scheduled: at(9)},
	}

	s := Summarize(pods, jobs, 16, at(100), Options{})

	require.Equal(t, 1, s.JobsByGangSize[0].Complete, "job a finished despite its lost attempts")
	require.Equal(t, 1, s.JobsByGangSize[0].Running, "job b is still running its replacement")
}

func TestSummarizeEvicted(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	jobs := []Job{
		{Name: "default/a", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "a-[0-9]+"},
		{Name: "default/b", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "b-[0-9]+"},
	}
	pods := []Pod{
		// Placed, then deleted after 3600s: preempted. The replacement finishes normally.
		{Namespace: "default", Name: "a-0", GPUs: 8, Phase: "Deleted", Created: t0, Scheduled: t0, Finished: at(3600)},
		{Namespace: "default", Name: "a-0", GPUs: 8, Phase: "Succeeded", Created: at(3600), Scheduled: at(3601), Finished: at(3700)},
		// Deleted before it was ever placed: nothing was lost.
		{Namespace: "default", Name: "b-0", GPUs: 8, Phase: "Deleted", Created: t0, Finished: at(10)},
		// Deleted but owned by no submitted job.
		{Namespace: "other", Name: "x", GPUs: 4, Phase: "Deleted", Created: t0, Scheduled: t0, Finished: at(1800)},
	}

	s := Summarize(pods, jobs, 16, at(4000), Options{})

	require.Equal(t, 2, s.PodsEvicted)
	require.Equal(t, 1, s.JobsEvicted)
	require.InDelta(t, 8.0+2.0, s.EvictedGPUHours, 1e-9)

	s.ScaleTime(60)
	require.InDelta(t, 600.0, s.EvictedGPUHours, 1e-9)
}

func TestSummarizeEvictionsByPriority(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	jobs := []Job{
		{Name: "default/a", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "a-[0-9]+"},
		{Name: "default/g", Namespace: "default", Submitted: t0, Pods: 2, PodRegexp: "g-[0-9]+"},
		{Name: "default/b", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "b-[0-9]+"},
	}
	evicted := func(name string, prio int, gpus float64, start, end int) Pod {
		return Pod{Namespace: "default", Name: name, GPUs: gpus, Priority: prio, Phase: phaseDeleted,
			Created: at(start), Scheduled: at(start), Finished: at(end)}
	}
	pods := []Pod{
		// Job a, priority 50, is evicted three times: its pod restarts each time.
		evicted("a-0", 50, 4, 0, 3600), evicted("a-1", 50, 4, 3600, 7200), evicted("a-2", 50, 4, 7200, 10800),
		// Gang g, priority 100, loses both pods once: one round, not a repeat.
		evicted("g-0", 100, 8, 0, 3600), evicted("g-1", 100, 8, 0, 3600),
		// Job b, priority 50, is evicted once.
		evicted("b-0", 50, 2, 0, 3600),
	}

	s := Summarize(pods, jobs, 16, at(20000), Options{})

	require.Equal(t, []PriorityEvictions{
		{Priority: 50, Pods: 4, Jobs: 2, GPUHours: 3*4 + 2},
		{Priority: 100, Pods: 2, Jobs: 1, GPUHours: 16},
	}, s.EvictionsByPriority)
	require.Equal(t, 1, s.JobsEvictedAgain, "only job a lost more pods than it has")
	require.Equal(t, 3, s.MaxEvictionsPerJob)

	s.ScaleTime(60)
	require.Equal(t, 840.0, s.EvictionsByPriority[0].GPUHours)
}

func TestSummarizeNoEvictionsByPriority(t *testing.T) {
	s := Summarize(nil, nil, 8, time.Now(), Options{})
	require.Empty(t, s.EvictionsByPriority)
	require.Zero(t, s.JobsEvictedAgain)
	require.Zero(t, s.MaxEvictionsPerJob)
}

func TestIdleWaitingGPUHours(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	pods := []Pod{
		// Runs on half the cluster for 300s.
		{Name: "a", GPUs: 4, Created: at(0), Scheduled: at(0), Finished: at(300)},
		// Needs the whole cluster, so it waits from 50s until a finishes.
		{Name: "b", GPUs: 8, Created: at(50), Scheduled: at(300), Finished: at(400)},
		// Never placed and deleted at 100s; it waited the whole time.
		{Name: "c", GPUs: 2, Created: at(0), Finished: at(100)},
	}

	got := idleWaitingGPUHours(pods, 8, at(1000))

	// 0-50s: 4 idle, 2 waiting (c) -> 2; 50-100s: 4 idle, 10 waiting -> 4; 100-300s: 4 idle, 8 waiting -> 4;
	// 300-400s: nothing idle, nothing waiting.
	require.InDelta(t, (2*50.0+4*50+4*200)/3600, got, 1e-9)
	require.Zero(t, idleWaitingGPUHours(pods, 0, at(1000)), "no capacity, no waste")
	require.Zero(t, idleWaitingGPUHours(nil, 8, at(1000)))
}

func TestBindingGapGPUHours(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	gang := func(name string, pods int) Job {
		return Job{Name: "default/" + name, Namespace: "default", Submitted: t0, Pods: pods, PodRegexp: name + "-[a-z0-9]+"}
	}
	jobs := []Job{gang("g", 2), gang("h", 3), gang("s", 1)}
	pods := []Pod{
		// g: its first pod is bound at 10s and holds 8 GPUs until the second binds at 50s.
		{Namespace: "default", Name: "g-0", GPUs: 8, Phase: "Succeeded", Created: t0, Scheduled: at(10), Finished: at(200)},
		{Namespace: "default", Name: "g-1", GPUs: 8, Phase: "Succeeded", Created: t0, Scheduled: at(50), Finished: at(200)},
		// h needs 3 pods but only 2 ever bind: both hold their GPUs until the run ends at 1000s.
		{Namespace: "default", Name: "h-0", GPUs: 4, Created: t0, Scheduled: at(0)},
		{Namespace: "default", Name: "h-1", GPUs: 4, Created: t0, Scheduled: at(100)},
		// A single pod has no gang to wait for.
		{Namespace: "default", Name: "s-0", GPUs: 8, Phase: "Succeeded", Created: t0, Scheduled: at(300), Finished: at(400)},
		// An evicted gang pod is counted by the eviction metric, not here.
		{Namespace: "default", Name: "h-2", GPUs: 4, Phase: phaseDeleted, Created: t0, Scheduled: at(0), Finished: at(50)},
	}

	assignJobs(pods, jobs)
	got := bindingGapGPUHours(pods, jobs, at(1000))

	require.InDelta(t, (8*40.0+4*1000+4*900)/3600, got, 1e-9)
	require.Zero(t, bindingGapGPUHours(nil, jobs, at(1000)))
}

func TestSummarizeWasted(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	jobs := []Job{
		{Name: "default/a", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "a-[a-z0-9]+"},
		{Name: "default/g", Namespace: "default", Submitted: t0, Pods: 2, PodRegexp: "g-[a-z0-9]+"},
	}
	pods := []Pod{
		// Evicted after an hour on 4 GPUs: 4 GPU-hours lost.
		{Namespace: "default", Name: "a-0", GPUs: 4, Phase: phaseDeleted, Created: at(0), Scheduled: at(0), Finished: at(3600)},
		{Namespace: "default", Name: "a-1", GPUs: 4, Phase: "Succeeded", Created: at(3600), Scheduled: at(3600), Finished: at(4000)},
		// A gang whose first pod waits an hour for the second: 8 GPU-hours held.
		{Namespace: "default", Name: "g-0", GPUs: 8, Phase: "Succeeded", Created: at(0), Scheduled: at(0), Finished: at(5000)},
		{Namespace: "default", Name: "g-1", GPUs: 8, Phase: "Succeeded", Created: at(0), Scheduled: at(3600), Finished: at(5000)},
	}

	s := Summarize(pods, jobs, 16, at(10000), Options{})

	require.InDelta(t, 4.0, s.EvictedGPUHours, 1e-9)
	require.InDelta(t, 8.0, s.BindingGapGPUHours, 1e-9)
	require.InDelta(t, 12.0, s.WastedGPUHours, 1e-9, "evictions plus binding gaps")

	s.ScaleTime(60)
	require.InDelta(t, 480.0, s.BindingGapGPUHours, 1e-9)
	require.InDelta(t, 720.0, s.WastedGPUHours, 1e-9)
}

func TestBindingGapAfterEviction(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	jobs := []Job{{Name: "default/g", Namespace: "default", Submitted: t0, Pods: 2, PodRegexp: "g-[a-z0-9]+"}}

	// Both pods bind at once, so the gang never waited, even though one is evicted later.
	pods := []Pod{
		{Namespace: "default", Name: "g-0", GPUs: 1, Phase: "Succeeded", Created: t0, Scheduled: t0, Finished: at(100)},
		{Namespace: "default", Name: "g-1", GPUs: 1, Phase: phaseDeleted, Created: t0, Scheduled: t0, Finished: at(100)},
	}
	assignJobs(pods, jobs)
	require.Zero(t, bindingGapGPUHours(pods, jobs, at(1000)))

	// Evicted before its partner binds: the gang is short again until the replacement binds at 50s.
	pods = []Pod{
		{Namespace: "default", Name: "g-0", GPUs: 1, Phase: phaseDeleted, Created: t0, Scheduled: t0, Finished: at(5)},
		{Namespace: "default", Name: "g-1", GPUs: 1, Phase: "Succeeded", Created: t0, Scheduled: at(10), Finished: at(200)},
		{Namespace: "default", Name: "g-2", GPUs: 1, Phase: "Succeeded", Created: at(5), Scheduled: at(50), Finished: at(200)},
	}
	assignJobs(pods, jobs)
	require.InDelta(t, 40.0/3600, bindingGapGPUHours(pods, jobs, at(1000)), 1e-9)

	// The whole gang is evicted at 10s and its replacements bind at 20s and 30s, so the first
	// replacement waits 10s for its partner.
	pods = []Pod{
		{Namespace: "default", Name: "g-0", GPUs: 1, Phase: phaseDeleted, Created: t0, Scheduled: t0, Finished: at(10)},
		{Namespace: "default", Name: "g-1", GPUs: 1, Phase: phaseDeleted, Created: t0, Scheduled: t0, Finished: at(10)},
		{Namespace: "default", Name: "g-2", GPUs: 1, Phase: "Succeeded", Created: at(10), Scheduled: at(20), Finished: at(100)},
		{Namespace: "default", Name: "g-3", GPUs: 1, Phase: "Succeeded", Created: at(10), Scheduled: at(30), Finished: at(100)},
	}
	assignJobs(pods, jobs)
	require.InDelta(t, 10.0/3600, bindingGapGPUHours(pods, jobs, at(1000)), 1e-9)
}

func TestSizes(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	job := func(n string, pods int) Job {
		return Job{Name: "default/" + n, Namespace: "default", Submitted: t0, Pods: pods, PodRegexp: n + "-[0-9]+"}
	}
	jobs := []Job{job("s", 1), job("g", 2), job("n", 1)}
	pods := []Pod{
		{Namespace: "default", Name: "s-0", GPUs: 1, Created: t0, Scheduled: at(0), Finished: at(100)},
		// The gang holds 8 GPUs for 40s before its second pod lands, then runs 60s.
		{Namespace: "default", Name: "g-0", GPUs: 8, Created: t0, Scheduled: at(10), Finished: at(110)},
		{Namespace: "default", Name: "g-1", GPUs: 8, Created: t0, Scheduled: at(50), Finished: at(110)},
		{Namespace: "default", Name: "n-0", GPUs: 8, Created: t0},
	}

	s := Summarize(pods, jobs, 16, at(200), Options{MaxNodeGPUs: 8, BSLDFloor: time.Second})

	require.InDelta(t, 8.0*40/3600, s.BindingGapGPUHours, 1e-9)
	require.Equal(t, 1, s.JobsNeverAdmitted)
	require.Equal(t, 0, s.JobsAbandoned)
	require.Nil(t, s.BSLDMacro, "a job never ran")
	// Footprint 1 waited 0s, footprint 16 waited 50s: perfectly ranked.
	require.InDelta(t, 1.0, *s.FootprintWaitSpearman, 1e-9)
	require.Nil(t, s.LargeJobStarvationRatio, "single-GPU wait is zero")
	require.Equal(t, "17+", s.WaitByFootprint[5].Bucket)
	require.Equal(t, 0, s.WaitByFootprint[5].Jobs)
	require.Equal(t, 1, s.WaitByFootprint[4].Started)
	// (50+60)/60 for the gang; the single GPU job is clamped from 1 up to 1.
	jobs = jobs[:2]
	s = Summarize(pods[:3], jobs, 16, at(200), Options{MaxNodeGPUs: 8, BSLDFloor: time.Second})
	require.InDelta(t, (110.0/60+1)/2, *s.BSLDMacro, 1e-9)
}

func TestAbandoned(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jobs := []Job{{Name: "default/a", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "a-[0-9]+"}}
	pods := []Pod{{Namespace: "default", Name: "a-0", GPUs: 4, Created: t0, Scheduled: t0}}

	s := Summarize(pods, jobs, 4, t0.Add(time.Hour), Options{})

	require.Equal(t, 1, s.JobsAbandoned)
	require.InDelta(t, 4.0, s.AbandonedGPUHours, 1e-9)
}

func TestSpearmanTies(t *testing.T) {
	require.Nil(t, spearman([]float64{1, 1, 1}, []float64{1, 2, 3}))
	require.InDelta(t, -1.0, *spearman([]float64{1, 2, 3}, []float64{9, 5, 1}), 1e-9)
}
