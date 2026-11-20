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

package verdict_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/eventlog"
	"github.com/NVIDIA/knavigator/pkg/verdict"
)

// victim is a pod of the victim job, running from 2 s until end.
func victim(name, node string, end float64, by eventlog.Transition) pod {
	return pod{name: name, job: "victim", tenant: "t1", gpus: 8, node: node,
		created: 0, scheduled: 1, running: 2, end: end, endBy: by}
}

// high is a pod of the high-priority job, created at 10 s and never ended.
func high(name, node string, scheduled float64) pod {
	return pod{name: name, job: "high", tenant: "t1", gpus: 8, node: node,
		created: 10, scheduled: scheduled, running: scheduled + 0.5, end: -1}
}

func TestPreemption(t *testing.T) {
	params := verdict.PreemptionParams{HighJob: "high", HighPods: 2, VictimJob: "victim", GraceSec: 5}
	tests := []struct {
		name string
		tl   verdict.Timeline
		want want
		err  string
	}{
		{
			name: "supported when the whole victim workload is evicted",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 10.5, eventlog.Deleted), victim("v1", "n2", 10.5, eventlog.Deleted),
				victim("v2", "n3", 10.5, eventlog.Deleted), victim("v3", "n4", 10.5, eventlog.Deleted),
				high("h0", "n1", 11.0), high("h1", "n2", 11.2),
			), "preemption", at(60)),
			want: want{
				outcome: verdict.Supported,
				reason:  "no victim pod was left running 5 s after the high-priority job was scheduled",
				some:    map[string]float64{"victim_pods_before": 4, "victim_pods_evicted": 4, "victim_pods_remaining": 0},
				near:    map[string]float64{"gpu_seconds_lost": 272, "high_time_to_schedule_s": 1.2},
			},
		},
		{
			name: "partial when only some victim pods are evicted, ignoring recreated ones",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 10.3, eventlog.Preempted), victim("v1", "n2", 10.3, eventlog.Preempted),
				victim("v2", "n3", -1, ""), victim("v3", "n4", -1, ""),
				pod{name: "v4", job: "victim", tenant: "t1", gpus: 8, node: "n3", created: 10.4, scheduled: 30, running: 30.5, end: -1},
				high("h0", "n1", 11.0), high("h1", "n2", 11.2),
			), "preemption", at(60)),
			want: want{
				outcome: verdict.Partial,
				reason:  "2 of 4 victim pods were left running 5 s after the high-priority job was scheduled",
				near:    map[string]float64{"gpu_seconds_lost": 132.8},
			},
		},
		{
			name: "a victim that ends exactly at the grace counts as evicted",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 16.2, eventlog.Deleted), victim("v1", "n2", 10.5, eventlog.Deleted),
				victim("v2", "n3", 10.5, eventlog.Deleted), victim("v3", "n4", 10.5, eventlog.Deleted),
				high("h0", "n3", 11.0), high("h1", "n4", 11.2),
			), "preemption", at(60)),
			want: want{outcome: verdict.Supported},
		},
		{
			name: "not supported when the high-priority job never gets all its pods",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", -1, ""), victim("v1", "n2", -1, ""),
				victim("v2", "n3", -1, ""), victim("v3", "n4", -1, ""),
				pending("h0", "high", 10), pending("h1", "high", 10),
			), "preemption", at(60)),
			want: want{outcome: verdict.NotSupported, reason: "the high-priority job did not get all 2 pods scheduled"},
		},
		{
			// the victims succeed at 20 s, before the high-priority pods are
			// scheduled at 21 s: nothing was evicted, so preemption was not tested
			name: "a victim that completes on its own is a harness error",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 20, eventlog.Succeeded), victim("v1", "n2", 20, eventlog.Succeeded),
				victim("v2", "n3", 20, eventlog.Succeeded), victim("v3", "n4", 20, eventlog.Succeeded),
				high("h0", "n1", 21), high("h1", "n2", 21),
			), "preemption", at(60)),
			err: "victim pod v0 completed instead of being evicted, so the probe did not exercise preemption",
		},
		{
			// v4 is scheduled at 10 s, the instant the high-priority job is
			// created, and never ends: it is not a victim considered
			name: "a victim pod scheduled as the high-priority job is created is not counted",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 10.5, eventlog.Deleted), victim("v1", "n2", 10.5, eventlog.Deleted),
				victim("v2", "n3", 10.5, eventlog.Deleted), victim("v3", "n4", 10.5, eventlog.Deleted),
				pod{name: "v4", job: "victim", tenant: "t1", gpus: 8, node: "n5", created: 9, scheduled: 10, running: 10.5, end: -1},
				high("h0", "n1", 11.0), high("h1", "n2", 11.2),
			), "preemption", at(60)),
			want: want{
				outcome: verdict.Supported,
				reason:  "no victim pod was left running 5 s after the high-priority job was scheduled",
				some:    map[string]float64{"victim_pods_before": 4, "victim_pods_remaining": 0},
			},
		},
		{
			// v3 is scheduled and deleted without ever running: it counts as
			// evicted but lost no GPU-time. Lost: 3 pods * 8 GPUs * 8.5 s = 204.
			name: "a victim pod that never ran loses no GPU-seconds",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 10.5, eventlog.Deleted), victim("v1", "n2", 10.5, eventlog.Deleted),
				victim("v2", "n3", 10.5, eventlog.Deleted),
				pod{name: "v3", job: "victim", tenant: "t1", gpus: 8, node: "n4", created: 0, scheduled: 1, running: -1, end: 10.5, endBy: eventlog.Deleted},
				high("h0", "n1", 11.0), high("h1", "n2", 11.2),
			), "preemption", at(60)),
			want: want{
				outcome: verdict.Supported,
				some:    map[string]float64{"victim_pods_evicted": 4},
				near:    map[string]float64{"gpu_seconds_lost": 204},
			},
		},
		{
			// v0 succeeds at 5 s, before the high-priority job is created at
			// 10 s; v1 runs until it is deleted at 10.5 s. Only v1 was a victim
			// when the high-priority job arrived. Lost: 8 GPUs * 8.5 s = 68.
			name: "a victim pod that succeeded before the high-priority job arrived is not a victim",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 5, eventlog.Succeeded), victim("v1", "n2", 10.5, eventlog.Deleted),
				high("h0", "n1", 11.0), high("h1", "n2", 11.2),
			), "preemption", at(60)),
			want: want{
				outcome: verdict.Supported,
				reason:  "no victim pod was left running 5 s after the high-priority job was scheduled",
				some:    map[string]float64{"victim_pods_before": 1, "victim_pods_evicted": 1, "victim_pods_remaining": 0},
				near:    map[string]float64{"gpu_seconds_lost": 68, "high_time_to_schedule_s": 1.2},
			},
		},
		{
			// v0 fails at 5 s; v1 and v2 are deleted at 10.5 s. Counting v0 would
			// give 3 evicted and 24 + 68 + 68 = 160 GPU-seconds lost.
			name: "a victim pod that failed before the high-priority job arrived adds no eviction or lost time",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 5, eventlog.Failed), victim("v1", "n2", 10.5, eventlog.Deleted),
				victim("v2", "n3", 10.5, eventlog.Deleted),
				high("h0", "n1", 11.0), high("h1", "n2", 11.2),
			), "preemption", at(60)),
			want: want{
				outcome: verdict.Supported,
				some:    map[string]float64{"victim_pods_before": 2, "victim_pods_evicted": 2, "victim_pods_remaining": 0},
				near:    map[string]float64{"gpu_seconds_lost": 136},
			},
		},
		{
			// v0 is deleted at 10 s, the instant the high-priority job is created
			name: "a victim pod that ended as the high-priority job was created is not a victim",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 10, eventlog.Deleted), victim("v1", "n2", 10.5, eventlog.Deleted),
				high("h0", "n1", 11.0), high("h1", "n2", 11.2),
			), "preemption", at(60)),
			want: want{
				outcome: verdict.Supported,
				some:    map[string]float64{"victim_pods_before": 1, "victim_pods_evicted": 1},
				near:    map[string]float64{"gpu_seconds_lost": 68},
			},
		},
		{
			// the high-priority pods are scheduled at 11 s, so the grace ends at
			// 16 s; the window ends at 12 s, and the victims' deletions at 14 s
			// fall outside it. Nothing says whether they were still running at 16 s.
			name: "a window that ends before the grace, with a victim not yet ended, is a harness error",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 14, eventlog.Deleted), victim("v1", "n2", 14, eventlog.Deleted),
				victim("v2", "n3", 14, eventlog.Deleted), victim("v3", "n4", 14, eventlog.Deleted),
				high("h0", "n1", 11), high("h1", "n2", 11),
			), "preemption", at(12)),
			err: "the window ends 1 s after the high-priority job was scheduled, before the 5 s grace, and victim pod v0 has no recorded end",
		},
		{
			name: "the same run with a complete window is supported",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 14, eventlog.Deleted), victim("v1", "n2", 14, eventlog.Deleted),
				victim("v2", "n3", 14, eventlog.Deleted), victim("v3", "n4", 14, eventlog.Deleted),
				high("h0", "n1", 11), high("h1", "n2", 11),
			), "preemption", at(60)),
			want: want{
				outcome: verdict.Supported,
				some:    map[string]float64{"victim_pods_before": 4, "victim_pods_evicted": 4, "victim_pods_remaining": 0},
			},
		},
		{
			// v0 is deleted at 11.5 s, inside the 12 s window; v1's deletion at
			// 14 s falls outside it, so v1 is the victim that cannot be judged
			name: "a window that ends before the grace is a harness error for any victim not yet ended, not only the first",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 11.5, eventlog.Deleted), victim("v1", "n2", 14, eventlog.Deleted),
				high("h0", "n1", 11), high("h1", "n2", 11),
			), "preemption", at(12)),
			err: "the window ends 1 s after the high-priority job was scheduled, before the 5 s grace, and victim pod v1 has no recorded end",
		},
		{
			// every victim is deleted at 11.5 s, inside a window that ends at 12 s
			name: "a window that ends before the grace still judges victims all ended within it",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 11.5, eventlog.Deleted), victim("v1", "n2", 11.5, eventlog.Deleted),
				high("h0", "n1", 11), high("h1", "n2", 11),
			), "preemption", at(12)),
			want: want{
				outcome: verdict.Supported,
				some:    map[string]float64{"victim_pods_before": 2, "victim_pods_evicted": 2, "victim_pods_remaining": 0},
			},
		},
		{
			// the window ends at 16 s, exactly when the grace does
			name: "a window that ends at the grace judges a victim still running as left running",
			tl: mustBuild(fixture("preemption",
				victim("v0", "n1", 11.5, eventlog.Deleted), victim("v1", "n2", -1, ""),
				high("h0", "n1", 11), high("h1", "n3", 11),
			), "preemption", at(16)),
			want: want{
				outcome: verdict.Partial,
				reason:  "1 of 2 victim pods were left running 5 s after the high-priority job was scheduled",
			},
		},
		{
			name: "no victim running first is a harness error",
			tl: mustBuild(fixture("preemption",
				pending("v0", "victim", 0), high("h0", "n1", 11.0), high("h1", "n2", 11.2),
			), "preemption", at(60)),
			err: "no victim pod was running when the high-priority job was created",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := verdict.Preemption(tc.tl, params)
			if tc.err != "" {
				require.EqualError(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			check(t, v, tc.want)
		})
	}
}
