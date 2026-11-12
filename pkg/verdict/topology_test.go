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

	"github.com/NVIDIA/knavigator/pkg/verdict"
)

// attempt is one topology attempt: two pods of job, on nodeA and nodeB,
// scheduled one second after start.
func attempt(job, nodeA, nodeB string, start float64) []pod {
	return []pod{
		done(job+"-0", job, nodeA, start, start+1, start+1.5, start+20),
		done(job+"-1", job, nodeB, start, start+1, start+1.5, start+20),
	}
}

// attempts joins the pods of several attempts.
func attempts(ps ...[]pod) []pod {
	var all []pod
	for _, p := range ps {
		all = append(all, p...)
	}
	return all
}

func TestTopology(t *testing.T) {
	racks := map[string]string{"n1": "rack-a", "n2": "rack-a", "n3": "rack-b", "n4": "rack-b"}
	params := verdict.TopologyParams{AttemptJobs: []string{"t1", "t2", "t3"}, PodsPerJob: 2, RackOf: racks}
	run := func(ps ...[]pod) verdict.Timeline {
		return mustBuild(fixture("topology", attempts(ps...)...), "topology", at(200))
	}
	tests := []struct {
		name   string
		tl     verdict.Timeline
		params verdict.TopologyParams
		want   want
		err    string
	}{
		{
			name:   "supported when every attempt lands in one rack",
			tl:     run(attempt("t1", "n3", "n4", 0), attempt("t2", "n4", "n3", 30), attempt("t3", "n3", "n4", 60)),
			params: params,
			want: want{
				outcome:      verdict.Supported,
				reason:       "all 3 attempts placed every pod in one rack",
				measurements: map[string]float64{"attempts": 3, "attempts_one_rack": 3},
			},
		},
		{
			name:   "partial when some attempts split across racks",
			tl:     run(attempt("t1", "n3", "n4", 0), attempt("t2", "n2", "n3", 30), attempt("t3", "n3", "n4", 60)),
			params: params,
			want:   want{outcome: verdict.Partial, reason: "2 of 3 attempts placed every pod in one rack"},
		},
		{
			name:   "not supported when no attempt lands in one rack",
			tl:     run(attempt("t1", "n2", "n3", 0), attempt("t2", "n2", "n4", 30), attempt("t3", "n2", "n3", 60)),
			params: params,
			want:   want{outcome: verdict.NotSupported, reason: "no attempt placed its pods in one rack"},
		},
		{
			// t1-2 is created first but scheduled last, in rack A; the first
			// two scheduled pods are both in rack B
			name: "only the first pods_per_job pods to be scheduled count",
			tl: run([]pod{
				done("t1-2", "t1", "n1", 0, 5, 5.5, 20),
				done("t1-0", "t1", "n3", 0.5, 1, 1.5, 20),
				done("t1-1", "t1", "n4", 0.5, 1.2, 1.7, 20),
			}),
			params: verdict.TopologyParams{AttemptJobs: []string{"t1"}, PodsPerJob: 2, RackOf: racks},
			want: want{
				outcome:      verdict.Supported,
				reason:       "all 1 attempts placed every pod in one rack",
				measurements: map[string]float64{"attempts": 1, "attempts_one_rack": 1},
			},
		},
		{
			// t1 lands in rack B; t2 has one pod on n3 and one pending; a queue
			// holds t3 as a Job with no pods
			name: "an attempt a queue holds, or one half placed, is not one rack",
			tl: mustBuild(fixtureJobs("topology", []job{{name: "t3", tenant: "t1", gpus: 16, created: 60, end: -1}},
				append(attempt("t1", "n3", "n4", 0),
					done("t2-0", "t2", "n3", 30, 31, 31.5, 50), pending("t2-1", "t2", 30))...),
				"topology", at(200)),
			params: params,
			want: want{
				outcome:      verdict.Partial,
				reason:       "1 of 3 attempts placed every pod in one rack",
				measurements: map[string]float64{"attempts": 3, "attempts_one_rack": 1},
			},
		},
		{
			name: "an attempt with no events is a harness error",
			tl:   run(attempt("t1", "n3", "n4", 0), attempt("t2", "n4", "n3", 30)),
			params: verdict.TopologyParams{AttemptJobs: []string{"t1", "t2", "topo-3"}, PodsPerJob: 2,
				RackOf: racks},
			err: "no events recorded for job topo-3",
		},
		{
			name:   "a node outside the fleet is a harness error",
			tl:     run(attempt("t1", "n3", "n9", 0), attempt("t2", "n3", "n4", 30), attempt("t3", "n3", "n4", 60)),
			params: params,
			err:    `node "n9" is not in rack_of`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := verdict.Topology(tc.tl, tc.params)
			if tc.err != "" {
				require.EqualError(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			check(t, v, tc.want)
		})
	}
}
