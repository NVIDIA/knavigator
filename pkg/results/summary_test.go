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
}

func TestCheck(t *testing.T) {
	require.NoError(t, Summary{Jobs: 2, JobsStarted: 2}.check(true))
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
	s := Summary{Sizes: Sizes{GangStrandedGPUHours: 1, AbandonedGPUHours: 2,
		WaitByFootprint: []Footprint{{WaitSec: Stats{Mean: 1, P95: 2, Max: 3}, BSLD: Stats{Mean: 1.5}}}}}
	s.ScaleTime(10)
	require.Equal(t, 10.0, s.GangStrandedGPUHours)
	require.Equal(t, 20.0, s.AbandonedGPUHours)
	require.Equal(t, Stats{Mean: 10, P95: 20, Max: 30}, s.WaitByFootprint[0].WaitSec)
	require.Equal(t, 1.5, s.WaitByFootprint[0].BSLD.Mean, "slowdown is a ratio")
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
	pods := []Pod{{Namespace: "default", Name: "a-0", Created: t0, Scheduled: t0, Finished: t0.Add(time.Second)}}

	s := Summarize(pods, jobs, 8, t0.Add(time.Minute), Options{})

	require.Equal(t, Stats{Count: 1}, s.JobWaitSec, "wait is never negative")
	require.Equal(t, Stats{Count: 1}, s.JobsByGangSize[0].WaitSec, "wait is never negative")
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

	require.InDelta(t, 8.0*40/3600, s.GangStrandedGPUHours, 1e-9)
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
