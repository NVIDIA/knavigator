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

// tenantPod is a pod of tenant t with g GPUs; negative seconds mean never.
func tenantPod(name, t string, g int64, created, running, end float64) pod {
	p := pod{name: name, job: name, tenant: t, gpus: g, node: "n1", created: created,
		scheduled: running, running: running, end: end, endBy: eventlog.Succeeded}
	if end < 0 {
		p.endBy = ""
	}
	return p
}

// aPods is tenant A's four running pods (0.5 to 60 s) and one queued pod
// per given second, starting then (negative: never) and running 60 s.
func aPods(queuedStart ...float64) []pod {
	var out []pod
	for _, n := range []string{"a0", "a1", "a2", "a3"} {
		out = append(out, tenantPod(n, "a", 8, 0, 0.5, 60))
	}
	for i, s := range queuedStart {
		n := []string{"a4", "a5", "a6", "a7"}[i]
		end := -1.0
		if s >= 0 {
			end = s + 60
		}
		out = append(out, tenantPod(n, "a", 8, 0, s, end))
	}
	return out
}

// bPods is one pod of tenant B per given second, submitted at 10 s,
// starting then (negative: never) and running 60 s.
func bPods(starts ...float64) []pod {
	var out []pod
	for i, s := range starts {
		n := []string{"b0", "b1", "b2", "b3"}[i]
		end := -1.0
		if s >= 0 {
			end = s + 60
		}
		out = append(out, tenantPod(n, "b", 8, 10, s, end))
	}
	return out
}

func TestFairShare(t *testing.T) {
	params := verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 16, JobLengthSec: 60,
		ToleranceSec: 5, CapacityGPUs: 32, WindowSec: 10}
	run := func(until float64, pods ...pod) verdict.Timeline {
		return verdict.Build(fixture("fair-share", pods...), "fair-share", at(until))
	}
	tests := []struct {
		name   string
		tl     verdict.Timeline
		params verdict.FairShareParams
		want   want
		err    string
	}{
		{
			name:   "supported when B reaches its entitlement within one job length",
			tl:     run(200, append(aPods(60.5, 60.5, 120.5, 120.5), bPods(60.5, 60.5, 120.5, 120.5)...)...),
			params: params,
			want: want{
				outcome: verdict.Supported,
				reason:  "tenant b had 16 GPUs running within 65 s of submitting; its entitlement is 16",
				some:    map[string]float64{"b_max_gpus_by_check": 16, "check_offset_s": 65},
			},
		},
		{
			name:   "partial when B gets some but not all of its entitlement",
			tl:     run(250, append(aPods(60.5, 60.5, 60.5, 120.5), bPods(60.5, 120.5, 120.5, 180.5)...)...),
			params: params,
			want: want{
				outcome: verdict.Partial,
				reason:  "tenant b had at most 8 of its 16 entitled GPUs running within 65 s of submitting",
			},
		},
		{
			name:   "not supported when B waits for A's whole backlog",
			tl:     run(200, append(aPods(60.5, 60.5, 60.5, 60.5), bPods(120.5, 120.5, 120.5, 120.5)...)...),
			params: params,
			want: want{
				outcome: verdict.NotSupported,
				reason:  "tenant b had no GPUs running within 65 s of submitting",
			},
		},
		{
			// a queue reclaimed for B at 12 s: b0 and b1 ran 12 to 72 s; the next
			// wave starts at 75.1 s, after the check at 10 + 65 = 75 s. At the
			// instant of the check B runs nothing; within the window it had 16.
			name: "the entitlement reached early counts, even if B is between waves at the check",
			tl: run(140, tenantPod("a0", "a", 8, 0, 0.5, 60), tenantPod("a1", "a", 8, 0, 0.5, 60),
				tenantPod("b0", "b", 8, 10, 12, 72), tenantPod("b1", "b", 8, 10, 12, 72),
				tenantPod("b2", "b", 8, 10, 75.1, 135.1), tenantPod("b3", "b", 8, 10, 75.1, 135.1)),
			params: params,
			want:   want{outcome: verdict.Supported, some: map[string]float64{"b_max_gpus_by_check": 16}},
		},
		{
			name:   "a B pod that starts exactly at the check counts",
			tl:     run(200, append(aPods(60.5, 60.5, 120.5, 120.5), bPods(75, 75, 120.5, 120.5)...)...),
			params: params,
			want:   want{some: map[string]float64{"b_max_gpus_by_check": 16}},
		},
		{
			name: "a tenant B that matches no Job is a harness error",
			tl:   run(200, append(aPods(60.5, 60.5, 120.5, 120.5), bPods(10.5, 10.5)...)...),
			params: verdict.FairShareParams{TenantB: "B", EntitlementGPUs: 16, JobLengthSec: 60,
				ToleranceSec: 5, CapacityGPUs: 32, WindowSec: 10},
			err: "no submission recorded for tenant B",
		},
		{
			name:   "records how long after the first submission B arrived",
			tl:     run(200, append(aPods(), tenantPod("b0", "b", 8, 23.5, 24, 84), tenantPod("b1", "b", 8, 23.5, 24, 84))...),
			params: params,
			want:   want{some: map[string]float64{"b_after_first_submit_s": 23.5}},
		},
		{
			// b0 ends at 40 s as b1 starts: B never runs both at once
			name: "a B pod ending as another starts does not count both",
			tl: run(200, tenantPod("a0", "a", 8, 0, 0.5, 60),
				tenantPod("b0", "b", 8, 10, 12, 40), tenantPod("b1", "b", 8, 10, 40, 100)),
			params: params,
			want: want{
				outcome: verdict.Partial,
				reason:  "tenant b had at most 8 of its 16 entitled GPUs running within 65 s of submitting",
				some:    map[string]float64{"b_max_gpus_by_check": 8},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := verdict.FairShare(tc.tl, tc.params)
			if tc.err != "" {
				require.EqualError(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			check(t, v, tc.want)
		})
	}
}

// threeTenants is one 10 s window with tenants a, b and c, each with a
// 30-GPU Job from 0 s, running ga, gb and gc GPUs for the whole window.
func threeTenants(ga, gb, gc int64) verdict.Timeline {
	return verdict.Build(fixtureJobs("fair-share",
		[]job{{name: "a1", tenant: "a", gpus: 30, created: 0, end: -1}, {name: "b1", tenant: "b", gpus: 30, created: 0, end: -1},
			{name: "c1", tenant: "c", gpus: 30, created: 0, end: -1}},
		tenantPod("a1", "a", ga, 0, 0, 10), tenantPod("b1", "b", gb, 0, 0, 10), tenantPod("c1", "c", gc, 0, 0, 10),
	), "fair-share", at(10))
}

func TestFairnessOverTime(t *testing.T) {
	tests := []struct {
		name   string
		tl     verdict.Timeline
		params verdict.FairShareParams
		want   map[string]float64
	}{
		{
			// capacity 4 GPUs, 10 s windows. Tenant a: 4 GPUs from 0 to 20 s.
			// Tenant b: 4 GPUs, submitted at 5 s, running 20 to 30 s.
			// Windows [0,10) and [10,20): entitlement 20 each, a delivers 40, b 0:
			// x = (2, 0), J = 0.5. Window [20,30): only b has demand, excluded from
			// J; b's shortfall is 20 + 20 + 0 = 40 GPU-seconds.
			name: "per-window Jain's index and the worst shortfall, by hand",
			tl: verdict.Build(fixture("fair-share",
				tenantPod("a1", "a", 4, 0, 0, 20), tenantPod("b1", "b", 4, 5, 20, 30),
			), "fair-share", at(30)),
			params: verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 4, JobLengthSec: 10,
				ToleranceSec: 0, CapacityGPUs: 4, WindowSec: 10},
			want: map[string]float64{"jain_median": 0.5, "jain_p5": 0.5, "windows_used": 2,
				"windows_excluded": 1, "worst_shortfall_gpu_s": 40},
		},
		{
			// capacity 8, 10 s windows, horizon 30 s. Tenant a: one 4-GPU pod running
			// 0 to 30 s. Tenant b: a 4-GPU Job submitted at 0 that a queue holds
			// without a pod until 20 s, then runs to 30 s. Demand starts at the
			// Job's submission, so b wants 40 GPU-s in every window: windows 1 and 2
			// have entitlements 40 and 40, deliveries 40 and 0, x = (1, 0), J = 0.5;
			// window 3 has x = (1, 1), J = 1. Median 0.5, three windows used, and
			// b's shortfall is 40 + 40 = 80 GPU-s. Counting demand from the pod's
			// creation instead would leave b without demand in windows 1 and 2.
			name: "demand counts from the Job's submission, while a queue holds it without pods",
			tl: verdict.Build(fixtureJobs("fair-share",
				[]job{{name: "a1", tenant: "a", gpus: 4, created: 0, end: 30}, {name: "b1", tenant: "b", gpus: 4, created: 0, end: 30}},
				tenantPod("a1", "a", 4, 0, 0, 30), tenantPod("b1", "b", 4, 20, 20, 30),
			), "fair-share", at(30)),
			params: verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 4, JobLengthSec: 30,
				ToleranceSec: 0, CapacityGPUs: 8, WindowSec: 10},
			want: map[string]float64{"jain_median": 0.5, "windows_used": 3, "worst_shortfall_gpu_s": 80},
		},
		{
			// capacity 8, 10 s windows, horizon 30 s. Tenant a's 4-GPU Job is
			// created at 0 and recorded as ended at 10 s (its pod runs 0 to 10);
			// tenant b's 4-GPU pod runs 0 to 30. Window [0,10): x = (1, 1), J = 1.
			// Windows [10,20) and [20,30): a's demand ended with its Job, so only b
			// has demand: excluded from J, and b gets all it wants. Median 1, one
			// window used, no shortfall. Reading a's demand to the horizon instead
			// would give J = 0.5 in those windows and a shortfall of 80 GPU-s.
			name: "a tenant's demand ends when its Job's end is recorded",
			tl: verdict.Build(fixtureJobs("fair-share",
				[]job{{name: "a1", tenant: "a", gpus: 4, created: 0, end: 10}, {name: "b1", tenant: "b", gpus: 4, created: 0, end: -1}},
				tenantPod("a1", "a", 4, 0, 0, 10), tenantPod("b1", "b", 4, 0, 0, 30),
			), "fair-share", at(30)),
			params: verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 4, JobLengthSec: 10,
				ToleranceSec: 0, CapacityGPUs: 8, WindowSec: 10},
			want: map[string]float64{"jain_median": 1, "windows_used": 1, "worst_shortfall_gpu_s": 0},
		},
		{
			// capacity 4, 10 s windows, horizon 20 s, no Job events. Tenant a's
			// job a1 has two 2-GPU pods: one runs 0 to 10 s, one never runs, so
			// the job has not ended and wants 4 GPUs to the horizon. Tenant b's
			// 2-GPU pod runs 0 to 20 s. Window [0,10): entitlements 20 and 20,
			// deliveries 20 and 20, J = 1. Window [10,20): a delivers 0, b 20,
			// x = (0, 1), J = 0.5, and a falls 20 GPU-s short. Ending a's demand
			// with its first pod instead would leave that window to b alone.
			name: "a job seen only through its pods wants capacity until all of them end",
			tl: verdict.Build(fixture("fair-share",
				pod{name: "a1-0", job: "a1", tenant: "a", gpus: 2, node: "n1", created: 0, scheduled: 0, running: 0, end: 10, endBy: eventlog.Succeeded},
				pod{name: "a1-1", job: "a1", tenant: "a", gpus: 2, created: 0, scheduled: -1, running: -1, end: -1},
				tenantPod("b1", "b", 2, 0, 0, -1),
			), "fair-share", at(20)),
			params: verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 2, JobLengthSec: 10,
				ToleranceSec: 0, CapacityGPUs: 4, WindowSec: 10},
			want: map[string]float64{"windows_used": 2, "windows_excluded": 0, "jain_median": 0.75,
				"jain_p5": 0.5, "worst_shortfall_gpu_s": 20},
		},
		{
			// capacity 4, 10 s windows, horizon 20 s. Tenant a's 2-GPU Job is
			// created at 0 and its pod runs 5 to 20; tenant b's 2-GPU Job is created
			// at 10 and runs 10 to 20. Window [0,10): only a has demand (20 GPU-s),
			// entitled to all 20, delivered 10: excluded from J, yet a falls 10
			// short. Window [10,20): x = (1, 1), J = 1. The worst shortfall is a's
			// 10 GPU-s, from the window J leaves out.
			name: "the shortfall of a window with one tenant's demand counts",
			tl: verdict.Build(fixtureJobs("fair-share",
				[]job{{name: "a1", tenant: "a", gpus: 2, created: 0, end: -1}, {name: "b1", tenant: "b", gpus: 2, created: 10, end: -1}},
				tenantPod("a1", "a", 2, 0, 5, -1), tenantPod("b1", "b", 2, 10, 10, -1),
			), "fair-share", at(20)),
			params: verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 2, JobLengthSec: 10,
				ToleranceSec: 0, CapacityGPUs: 4, WindowSec: 10},
			want: map[string]float64{"worst_shortfall_gpu_s": 10, "windows_used": 1, "windows_excluded": 1},
		},
		{
			// capacity 4, 10 s windows, horizon 30 s. Tenants a and b each have a
			// 2-GPU Job from 0 s that never ends, and run 2 GPUs in [0,10) and in
			// [20,30), nothing in [10,20). Windows 1 and 3: x = (1, 1), J = 1.
			// Window 2: both want capacity and neither gets any, so Jain's index is
			// 0/0; the window is excluded rather than turning the percentiles NaN.
			name: "a window where tenants want capacity and none is delivered is excluded",
			tl: verdict.Build(fixtureJobs("fair-share",
				[]job{{name: "a1", tenant: "a", gpus: 2, created: 0, end: -1}, {name: "b1", tenant: "b", gpus: 2, created: 0, end: -1}},
				tenantPod("a1", "a", 2, 0, 0, 10), tenantPod("b1", "b", 2, 0, 0, 10),
				pod{name: "a1-1", job: "a1", tenant: "a", gpus: 2, node: "n1", created: 20, scheduled: 20, running: 20, end: 30, endBy: eventlog.Succeeded},
				pod{name: "b1-1", job: "b1", tenant: "b", gpus: 2, node: "n2", created: 20, scheduled: 20, running: 20, end: 30, endBy: eventlog.Succeeded},
			), "fair-share", at(30)),
			params: verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 2, JobLengthSec: 10,
				ToleranceSec: 0, CapacityGPUs: 4, WindowSec: 10},
			want: map[string]float64{"windows_used": 2, "windows_excluded": 1, "jain_median": 1, "jain_p5": 1},
		},
		{
			// capacity 4, two 10 s windows, tenants a and b each with a 4-GPU Job
			// from 0 s: demand 40 GPU-s each per window, entitlement 20 each.
			// Window [0,10): a runs 4 GPUs (40), b nothing: x = (2, 0), J = 4 / (2 * 4)
			// = 0.5. Window [10,20): a and b run 2 GPUs each (20, 20): x = (1, 1), J = 1.
			// Sorted J = [0.5, 1]: the even-count median is (0.5 + 1) / 2 = 0.75, and
			// p5 is J[ceil(0.05 * 2) - 1] = J[0] = 0.5.
			name: "the 5th percentile and the even-count median come from distinct windows",
			tl: verdict.Build(fixtureJobs("fair-share",
				[]job{{name: "a1", tenant: "a", gpus: 4, created: 0, end: -1}, {name: "b1", tenant: "b", gpus: 4, created: 0, end: -1}},
				tenantPod("a1", "a", 4, 0, 0, 10),
				pod{name: "a1-1", job: "a1", tenant: "a", gpus: 2, node: "n1", created: 10, scheduled: 10, running: 10, end: 20, endBy: eventlog.Succeeded},
				pod{name: "b1", job: "b1", tenant: "b", gpus: 2, node: "n2", created: 10, scheduled: 10, running: 10, end: 20, endBy: eventlog.Succeeded},
			), "fair-share", at(20)),
			params: verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 4, JobLengthSec: 10,
				ToleranceSec: 0, CapacityGPUs: 4, WindowSec: 10},
			want: map[string]float64{"windows_used": 2, "jain_median": 0.75, "jain_p5": 0.5},
		},
		{
			// the threeTenants window with a, b and c running 3, 1 and 2 GPUs:
			// x = 0.3, 0.1, 0.2 in name order. Rounding every step, sq = (0.09 +
			// 0.010000000000000002) + 0.04000000000000001 = 0.14 and J =
			// 0.8571428571428573. A fused multiply-add (sq += x*x in one rounding,
			// as arm64 may compile it) gives sq = 0.13999999999999999 and J =
			// 0.8571428571428575, so evaluations of one run on two architectures
			// would differ in the last bit.
			name: "each product is rounded before adding, so every architecture gives the same bits",
			tl:   threeTenants(3, 1, 2),
			params: verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 10, JobLengthSec: 10,
				ToleranceSec: 0, CapacityGPUs: 30, WindowSec: 10},
			want: map[string]float64{"jain_median": 0.8571428571428573},
		},
		{
			// capacity 4, one 10 s window; a runs 3 GPUs, b runs 1 GPU, both the
			// whole window. Capped water-filling gives entitlements 30 and 10, so
			// x = (1, 1) and J = 1. An uncapped equal split (20, 20) would give 0.8.
			name: "each tenant's entitlement is capped at its demand",
			tl: verdict.Build(fixture("fair-share",
				tenantPod("a1", "a", 3, 0, 0, 10), tenantPod("b1", "b", 1, 0, 0, 10),
			), "fair-share", at(10)),
			params: verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 1, JobLengthSec: 10,
				ToleranceSec: 0, CapacityGPUs: 4, WindowSec: 10},
			want: map[string]float64{"jain_median": 1, "worst_shortfall_gpu_s": 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := verdict.FairShare(tc.tl, tc.params)
			require.NoError(t, err)
			check(t, v, want{some: tc.want})
		})
	}
}

// The threeTenants window with a, b and c running 1, 2 and 3 GPUs: x = 0.1,
// 0.2, 0.3, and J = 0.36 / (3 * 0.14) = 6/7. In doubles the sum depends on
// the order: a, b, c gives (0.1 + 0.2) + 0.3 = 0.6000000000000001 and J =
// 0.8571428571428573; b, c, a gives 0.6 and J = 0.857142857142857. The sum
// runs in tenant name order (a, b, c), so every evaluation agrees, whatever
// order the map holds the tenants in.
func TestFairnessSumsInTenantNameOrder(t *testing.T) {
	p := verdict.FairShareParams{TenantB: "b", EntitlementGPUs: 10, JobLengthSec: 10,
		ToleranceSec: 0, CapacityGPUs: 30, WindowSec: 10}
	tl := threeTenants(1, 2, 3)
	for i := 0; i < 50; i++ {
		v, err := verdict.FairShare(tl, p)
		require.NoError(t, err)
		require.Equal(t, 0.8571428571428573, v.Measurements["jain_median"], "evaluation %d", i)
	}
}
