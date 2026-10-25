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

// queued returns the four queued low-priority pods, q1 to q4, scheduled at
// the given seconds (negative: never).
func queued(s1, s2, s3, s4 float64) []pod {
	var out []pod
	for i, s := range []float64{s1, s2, s3, s4} {
		name := []string{"q1", "q2", "q3", "q4"}[i]
		if s < 0 {
			out = append(out, pending(name, name, 1))
			continue
		}
		out = append(out, done(name, name, "n1", 1, s, s+0.5, s+20))
	}
	return out
}

func TestPriority(t *testing.T) {
	params := verdict.PriorityParams{HighJob: "high", QueuedJobs: []string{"q1", "q2", "q3", "q4"}}
	tests := []struct {
		name string
		tl   verdict.Timeline
		want want
	}{
		{
			name: "supported when the high-priority pod takes the first freed slot",
			tl: verdict.Build(fixture("priority", append(queued(40, 50, 60, 70),
				done("h0", "high", "n1", 3, 30, 30.5, 50))...), "priority", at(100)),
			want: want{
				outcome:      verdict.Supported,
				reason:       "the high-priority job was scheduled before every queued lower-priority job",
				measurements: map[string]float64{"queued_jobs": 4, "queued_before_high": 0, "high_time_to_schedule_s": 27},
			},
		},
		{
			name: "a queued pod scheduled at the same instant does not count as ahead",
			tl: verdict.Build(fixture("priority", append(queued(30, 50, 60, 70),
				done("h0", "high", "n2", 3, 30, 30.5, 50))...), "priority", at(100)),
			want: want{outcome: verdict.Supported},
		},
		{
			name: "partial when some queued pods go first",
			tl: verdict.Build(fixture("priority", append(queued(30, 40, 60, 70),
				done("h0", "high", "n1", 3, 50, 50.5, 70))...), "priority", at(100)),
			want: want{
				outcome: verdict.Partial,
				reason:  "2 of 4 queued lower-priority jobs were scheduled before the high-priority job",
			},
		},
		{
			// q1 has two pods, both scheduled before the high-priority pod (50 s);
			// q2 and q3 go first too, q4 after: 3 of the 4 queued jobs are ahead,
			// not "all 4" as counting pods (2 + 1 + 1) would say
			name: "a queued job with two pods ahead counts once",
			tl: verdict.Build(fixture("priority",
				done("q1-0", "q1", "n1", 1, 30, 30.5, 50), done("q1-1", "q1", "n2", 1, 31, 31.5, 51),
				done("q2", "q2", "n3", 1, 40, 40.5, 60), done("q3", "q3", "n4", 1, 45, 45.5, 65),
				done("q4", "q4", "n1", 1, 70, 70.5, 90), done("h0", "high", "n1", 3, 50, 50.5, 70),
			), "priority", at(100)),
			want: want{
				outcome: verdict.Partial,
				reason:  "3 of 4 queued lower-priority jobs were scheduled before the high-priority job",
				some:    map[string]float64{"queued_before_high": 3},
			},
		},
		{
			name: "not supported when the pods run in arrival order",
			tl: verdict.Build(fixture("priority", append(queued(30, 40, 50, 60),
				done("h0", "high", "n1", 3, 70, 70.5, 90))...), "priority", at(100)),
			want: want{
				outcome: verdict.NotSupported,
				reason:  "all 4 queued lower-priority jobs were scheduled before the high-priority job",
			},
		},
		{
			name: "not supported when the high-priority pod is never scheduled",
			tl: verdict.Build(fixture("priority", append(queued(30, 40, -1, -1),
				pending("h0", "high", 3))...), "priority", at(100)),
			want: want{
				outcome:      verdict.NotSupported,
				reason:       "the high-priority job was never scheduled",
				measurements: map[string]float64{"queued_jobs": 4, "queued_before_high": 2},
			},
		},
		{
			// a queue holds a queued Job without pods: the Job is created at
			// 2.2 s, its pod only at admission (33.24 s), scheduled at 34.26 s
			name: "time to schedule runs from the Job's submission, not its pod's creation",
			tl: verdict.Build(fixtureJobs("priority", []job{{name: "high", tenant: "t1", gpus: 8, created: 2.2, end: -1}},
				append(queued(44, 54, 64, 67),
					pod{name: "h0", job: "high", tenant: "t1", gpus: 8, node: "n1", created: 33.24, scheduled: 34.26, running: 34.3, end: -1})...),
				"priority", at(100)),
			want: want{outcome: verdict.Supported, near: map[string]float64{"high_time_to_schedule_s": 32.06}},
		},
		{
			name: "not supported when the high-priority job is held as a Job and never gets a pod",
			tl: verdict.Build(fixtureJobs("priority", []job{{name: "high", tenant: "t1", gpus: 8, created: 3, end: -1}},
				queued(30, 40, 50, 60)...), "priority", at(100)),
			want: want{outcome: verdict.NotSupported, reason: "the high-priority job was never scheduled"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := verdict.Priority(tc.tl, params)
			require.NoError(t, err)
			check(t, v, tc.want)
		})
	}
}
