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

	s := Summarize(pods, jobs, 16, at(100))

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
	s := Summarize(pods, jobs, 8, at(3600))
	require.Equal(t, 3600.0, s.MakespanSec)
	require.Equal(t, 1, s.JobsStarted)

	// A drained run ends at its last completion.
	s = Summarize(pods[:1], jobs[:1], 8, at(3600))
	require.Equal(t, 10.0, s.MakespanSec)
	require.InDelta(t, 1.0, s.GPUUtilization, 1e-9)
}
