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

func TestGang(t *testing.T) {
	params := verdict.GangParams{Jobs: []string{"gang-a", "gang-b"}, Size: 2, GraceSec: 5}
	tests := []struct {
		name   string
		tl     verdict.Timeline
		params verdict.GangParams
		want   want
		err    string
	}{
		{
			name: "supported when each gang is placed at once and both complete",
			tl: verdict.Build(fixture("gang",
				done("a0", "gang-a", "n1", 0, 1.0, 2, 22), done("a1", "gang-a", "n2", 0, 1.5, 2, 22),
				done("b0", "gang-b", "n1", 0, 22.5, 23.5, 44), done("b1", "gang-b", "n2", 0, 23.0, 24, 44),
			), "gang", at(50)),
			params: params,
			want: want{
				outcome:      verdict.Supported,
				reason:       "no gang was partly placed for more than 5 s, and all 2 gangs completed",
				measurements: map[string]float64{"gangs": 2, "gangs_completed": 2, "max_partial_s": 0.5},
				evidence:     []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			},
		},
		{
			name: "exactly the grace still counts as placed at once",
			tl: verdict.Build(fixture("gang",
				done("a0", "gang-a", "n1", 0, 1, 7, 27), done("a1", "gang-a", "n2", 0, 6, 7, 27),
				done("b0", "gang-b", "n1", 0, 28, 29, 49), done("b1", "gang-b", "n2", 0, 28, 29, 49),
			), "gang", at(60)),
			params: params,
			want:   want{outcome: verdict.Supported, some: map[string]float64{"max_partial_s": 5}},
		},
		{
			name: "partial when a gang is partly placed past the grace but every gang completes",
			tl: verdict.Build(fixture("gang-control",
				done("a0", "gang-a", "n1", 0, 1, 2, 22), done("a1", "gang-a", "n2", 0, 1, 2, 22),
				done("a2", "gang-a", "n1", 0, 22.5, 23, 43),
				done("b0", "gang-b", "n3", 0, 1, 2, 22), done("b1", "gang-b", "n4", 0, 1, 2, 22),
				done("b2", "gang-b", "n2", 0, 22.5, 23, 43),
			), "gang-control", at(60)),
			params: verdict.GangParams{Jobs: []string{"gang-a", "gang-b"}, Size: 3, GraceSec: 5},
			want: want{
				outcome: verdict.Partial,
				reason:  "gang gang-a was partly placed for 21.5 s; all 2 gangs completed",
				some:    map[string]float64{"max_partial_s": 21.5},
			},
		},
		{
			name: "one millisecond past the grace counts as partly placed",
			tl: verdict.Build(fixture("gang",
				done("a0", "gang-a", "n1", 0, 1, 7.001, 27), done("a1", "gang-a", "n2", 0, 6.001, 7.001, 27),
				done("b0", "gang-b", "n1", 0, 28, 29, 49), done("b1", "gang-b", "n2", 0, 28, 29, 49),
			), "gang", at(60)),
			params: params,
			want:   want{outcome: verdict.Partial, near: map[string]float64{"max_partial_s": 5.001}},
		},
		{
			name: "not supported when a partial placement lasts until the probe ends",
			tl: verdict.Build(fixture("gang",
				pod{name: "a0", job: "gang-a", tenant: "t1", gpus: 8, node: "n1", created: 0, scheduled: 1, running: 2, end: -1},
				pending("a1", "gang-a", 0),
				done("b0", "gang-b", "n2", 0, 1, 2, 22), done("b1", "gang-b", "n3", 0, 1, 2, 22),
			), "gang", at(120)),
			params: params,
			want: want{
				outcome: verdict.NotSupported,
				reason:  "gang gang-a was partly placed for 119 s, and gang gang-a did not complete",
				some:    map[string]float64{"gangs_completed": 1},
			},
		},
		{
			name: "not supported when a gang never gets a pod placed",
			tl: verdict.Build(fixture("gang",
				done("a0", "gang-a", "n1", 0, 1, 2, 22), done("a1", "gang-a", "n2", 0, 1, 2, 22),
				pending("b0", "gang-b", 0), pending("b1", "gang-b", 0),
			), "gang", at(120)),
			params: params,
			want: want{
				outcome: verdict.NotSupported,
				reason:  "gang gang-b did not complete before the probe ended",
				some:    map[string]float64{"max_partial_s": 0},
			},
		},
		{
			name: "a deleted pod does not count as completed",
			tl: verdict.Build(fixture("gang",
				done("a0", "gang-a", "n1", 0, 1, 2, 22),
				pod{name: "a1", job: "gang-a", tenant: "t1", gpus: 8, node: "n2", created: 0, scheduled: 1, running: 2, end: 22, endBy: eventlog.Deleted},
				done("b0", "gang-b", "n1", 0, 23, 24, 44), done("b1", "gang-b", "n2", 0, 23, 24, 44),
			), "gang", at(50)),
			params: params,
			want: want{
				outcome: verdict.NotSupported,
				reason:  "gang gang-a did not complete before the probe ended",
			},
		},
		{
			name: "not supported when a queue never admits a gang, so it has a Job and no pods",
			tl: verdict.Build(fixtureJobs("gang", []job{{name: "gang-b", tenant: "t1", gpus: 16, created: 0, end: -1}},
				done("a0", "gang-a", "n1", 0, 1, 2, 22), done("a1", "gang-a", "n2", 0, 1, 2, 22),
			), "gang", at(120)),
			params: params,
			want: want{
				outcome: verdict.NotSupported,
				reason:  "gang gang-b did not complete before the probe ended",
				hasSeq:  []int64{1},
			},
		},
		{
			name:   "a gang with no recorded event at all is a harness error",
			tl:     verdict.Build(fixture("gang", done("a0", "gang-a", "n1", 0, 1, 2, 22)), "gang", at(50)),
			params: params,
			err:    "no events recorded for job gang-b",
		},
		{
			// gang-a is partly placed for exactly the grace (1 to 6 s) and
			// completes; gang-b never gets a pod scheduled
			name: "a gang partly placed for exactly the grace is not in the reason",
			tl: verdict.Build(fixture("gang",
				done("a0", "gang-a", "n1", 0, 1, 7, 27), done("a1", "gang-a", "n2", 0, 6, 7, 27),
				pending("b0", "gang-b", 0), pending("b1", "gang-b", 0),
			), "gang", at(60)),
			params: params,
			want: want{
				outcome: verdict.NotSupported,
				reason:  "gang gang-b did not complete before the probe ended",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := verdict.Gang(tc.tl, tc.params)
			if tc.err != "" {
				require.EqualError(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			check(t, v, tc.want)
		})
	}
}
