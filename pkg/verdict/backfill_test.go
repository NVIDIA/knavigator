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

// blockers run on n1 to n3 and end at 50, 55 and 60 s, so the gang's
// earliest possible start is 60 s, the last blocker's end.
func blockers() []pod {
	return []pod{
		done("blk0", "blk0", "n1", 0, 0, 0.5, 50), done("blk1", "blk1", "n2", 0, 0, 0.5, 55),
		done("blk2", "blk2", "n3", 0, 0, 0.5, 60),
	}
}

// bigPod is one pod of the gang, scheduled at s and never ended.
func bigPod(name, node string, s float64) pod {
	return pod{name: name, job: "big", tenant: "t1", gpus: 8, node: node,
		created: 1, scheduled: s, running: s + 0.5, end: -1}
}

// bigPods is the gang's four pods on n1 to n4, all scheduled at s.
func bigPods(s float64) []pod {
	return []pod{bigPod("b0", "n1", s), bigPod("b1", "n2", s), bigPod("b2", "n3", s), bigPod("b3", "n4", s)}
}

// scenario is the blockers, the gang's pods scheduled at bigAt (negative:
// never), and short jobs s0 to s3 scheduled at the given seconds
// (negative: never) that run 25 s on n4.
func scenario(bigAt float64, smallAt ...float64) verdict.Timeline {
	ps := blockers()
	if bigAt < 0 {
		for _, n := range []string{"b0", "b1", "b2", "b3"} {
			ps = append(ps, pending(n, "big", 1))
		}
	} else {
		ps = append(ps, bigPods(bigAt)...)
	}
	for i, s := range smallAt {
		n := []string{"s0", "s1", "s2", "s3"}[i]
		if s < 0 {
			ps = append(ps, pending(n, n, 2))
			continue
		}
		ps = append(ps, done(n, n, "n4", 2, s, s+0.5, s+25))
	}
	return mustBuild(fixture("backfill", ps...), "backfill", at(200))
}

// backfillRun builds a backfill timeline from the blockers and pods.
func backfillRun(pods ...pod) verdict.Timeline {
	return mustBuild(fixture("backfill", append(blockers(), pods...)...), "backfill", at(200))
}

func TestBackfill(t *testing.T) {
	params := verdict.BackfillParams{BigJob: "big", BigPods: 4,
		BlockerJobs: []string{"blk0", "blk1", "blk2"}, SmallJobs: []string{"s0", "s1", "s2", "s3"}, ToleranceSec: 5}
	// b0 is scheduled at 61 s and b1 to b3 at 80 s: the gang starts at 80 s
	staggered := []pod{bigPod("b0", "n1", 61), bigPod("b1", "n2", 80), bigPod("b2", "n3", 80), bigPod("b3", "n4", 80)}
	for i, s := range []float64{2, 27.5, 62} {
		n := []string{"s0", "s1", "s2"}[i]
		staggered = append(staggered, done(n, n, "n4", 2, s, s+0.5, s+25))
	}
	staggered = append(staggered, pending("s3", "s3", 2))

	tests := []struct {
		name   string
		tl     verdict.Timeline
		params verdict.BackfillParams
		want   want
		err    string
	}{
		{
			name:   "supported when short jobs fill the gap and the gang starts on time",
			tl:     scenario(61, 2, 27.5, 61.5, 86.5),
			params: params,
			want: want{
				outcome:      verdict.Supported,
				reason:       "2 short jobs ran before the gang and held nodes 0 s past its earliest possible start (tolerance 5 s); it started 1 s after that start",
				measurements: map[string]float64{"small_before_big": 2, "big_start_delay_s": 1, "short_hold_s": 0, "big_start_after_free_s": 1},
			},
		},
		{
			// s0 has two pods, both scheduled before the gang (61 s); s1 also goes
			// first, s2 and s3 after: 2 short jobs ran first, not 3 as counting
			// pods would say. Delay 61 - 60 = 1 s, within the tolerance.
			name: "a short job with two pods counts once",
			tl: backfillRun(append(bigPods(61),
				done("s0-0", "s0", "n4", 2, 2, 2.5, 27), done("s0-1", "s0", "n4", 2, 3, 3.5, 28),
				done("s1", "s1", "n4", 2, 28, 28.5, 53),
				done("s2", "s2", "n1", 2, 62, 62.5, 87), done("s3", "s3", "n2", 2, 63, 63.5, 88))...),
			params: params,
			want: want{
				outcome:      verdict.Supported,
				reason:       "2 short jobs ran before the gang and held nodes 0 s past its earliest possible start (tolerance 5 s); it started 1 s after that start",
				measurements: map[string]float64{"small_before_big": 2, "big_start_delay_s": 1, "short_hold_s": 0, "big_start_after_free_s": 1},
			},
		},
		{
			// s1 runs 40 to 65: it holds a node 5 s past the earliest start (60 s);
			// s2 and s3 stay pending
			name:   "a hold exactly at the tolerance still counts as on time",
			tl:     scenario(80, 2, 40, -1, -1),
			params: params,
			want:   want{outcome: verdict.Supported, some: map[string]float64{"short_hold_s": 5}},
		},
		{
			// s1 runs 50 to 75, but the gang starts at 63: s1 held a node only
			// until 63, 3 s past the earliest start (60 s); s2 and s3 stay pending
			name:   "a short pod's hold is capped at the gang's start",
			tl:     scenario(63, 2, 50, -1, -1),
			params: params,
			want: want{
				outcome:      verdict.Supported,
				reason:       "2 short jobs ran before the gang and held nodes 3 s past its earliest possible start (tolerance 5 s); it started 3 s after that start",
				measurements: map[string]float64{"small_before_big": 2, "big_start_delay_s": 3, "short_hold_s": 3, "big_start_after_free_s": 0},
			},
		},
		{
			name: "a short pod that never ended holds a node until the gang's start",
			tl: backfillRun(append(bigPods(63),
				done("s0", "s0", "n4", 2, 2, 2.5, 27),
				pod{name: "s1", job: "s1", tenant: "t1", gpus: 8, node: "n4", created: 2, scheduled: 50, running: 50.5, end: -1},
				pending("s2", "s2", 2), pending("s3", "s3", 2))...),
			params: params,
			want: want{
				outcome:      verdict.Supported,
				reason:       "2 short jobs ran before the gang and held nodes 3 s past its earliest possible start (tolerance 5 s); it started 3 s after that start",
				measurements: map[string]float64{"small_before_big": 2, "big_start_delay_s": 3, "short_hold_s": 3, "big_start_after_free_s": 0},
			},
		},
		{
			name:   "partial when short jobs delay the gang past the tolerance",
			tl:     scenario(78, 2, 27.5, 52.5, 78.5),
			params: params,
			want: want{
				outcome: verdict.Partial,
				reason:  "3 short jobs ran before the gang and held nodes 17.5 s past its earliest possible start (tolerance 5 s); it started 18 s after that start",
			},
		},
		{
			// s1 runs 52.5 to 77.5 on n4, 17.5 s past the earliest start (60 s);
			// s2 runs 56 to 61 on n1, 1 s past it; s3 stays pending. The longest
			// hold comes first both in job order and in time, so a rule that kept
			// the last hold would read 1 s and call the gang on time.
			name: "the longest hold wins when a later short pod holds a node for less",
			tl: backfillRun(append(bigPods(80),
				done("s0", "s0", "n4", 2, 2, 2.5, 27),
				done("s1", "s1", "n4", 2, 52.5, 53, 77.5),
				done("s2", "s2", "n1", 2, 56, 56.5, 61),
				pending("s3", "s3", 2))...),
			params: params,
			want: want{
				outcome:      verdict.Partial,
				reason:       "3 short jobs ran before the gang and held nodes 17.5 s past its earliest possible start (tolerance 5 s); it started 20 s after that start",
				measurements: map[string]float64{"small_before_big": 3, "big_start_delay_s": 20, "short_hold_s": 17.5, "big_start_after_free_s": 2.5},
			},
		},
		{
			// s1 is scheduled at 80 s, when the gang is: it did not run first and
			// was not in the gang's way, although it then runs to 105 s; s2 and
			// s3 stay pending
			name:   "a short pod scheduled at the same instant as the gang is left out",
			tl:     scenario(80, 2, 80, -1, -1),
			params: params,
			want: want{
				outcome:      verdict.Supported,
				reason:       "1 short jobs ran before the gang and held nodes 0 s past its earliest possible start (tolerance 5 s); it started 20 s after that start",
				measurements: map[string]float64{"small_before_big": 1, "big_start_delay_s": 20, "short_hold_s": 0, "big_start_after_free_s": 20},
			},
		},
		{
			name:   "partial when short jobs ran and the gang never started",
			tl:     scenario(-1, 2, 27.5, 52.5, 78),
			params: params,
			want:   want{outcome: verdict.Partial, reason: "4 short jobs ran, and the gang never started"},
		},
		{
			// s0 and s1 end at 27 and 52.5 s, before the earliest start (60 s);
			// the gang's 16 s are the scheduler's own latency; s2 and s3 stay pending
			name:   "supported when the gang starts late with no short job in its way",
			tl:     scenario(76, 2, 27.5, -1, -1),
			params: params,
			want: want{
				outcome:      verdict.Supported,
				reason:       "2 short jobs ran before the gang and held nodes 0 s past its earliest possible start (tolerance 5 s); it started 16 s after that start",
				measurements: map[string]float64{"small_before_big": 2, "big_start_delay_s": 16, "short_hold_s": 0, "big_start_after_free_s": 16},
			},
		},
		{
			// s3 runs 61 to 86: 26 s past the earliest start; the gang starts at
			// 101 s, 15 s after s3 frees its node
			name:   "the short jobs' hold is separate from the gang's own start latency",
			tl:     scenario(101, 2, 27.5, 52.5, 61),
			params: params,
			want: want{
				outcome: verdict.Partial,
				some:    map[string]float64{"short_hold_s": 26, "big_start_after_free_s": 15, "big_start_delay_s": 41},
			},
		},
		{
			name:   "the gang starts at its BigPods-th pod",
			tl:     backfillRun(staggered...),
			params: params,
			want:   want{outcome: verdict.Partial, some: map[string]float64{"big_start_delay_s": 20}},
		},
		{
			name: "a blocker job with no pods is a harness error",
			tl:   scenario(61, 2, 27.5, 61.5, 86.5),
			params: verdict.BackfillParams{BigJob: "big", BigPods: 4,
				BlockerJobs: []string{"blocker-1", "blk1", "blk2"}, SmallJobs: []string{"s0", "s1", "s2", "s3"}, ToleranceSec: 5},
			err: "no pods recorded for blocker job blocker-1",
		},
		{
			name: "a blocker job that never finished is a harness error",
			tl: mustBuild(fixture("backfill",
				done("blk0", "blk0", "n1", 0, 0, 0.5, 50), done("blk1", "blk1", "n2", 0, 0, 0.5, 55),
				done("blk2", "blk2", "n3", 0, 0, 0.5, -1),
				pending("b0", "big", 1), done("s0", "s0", "n4", 2, 2, 2.5, 27),
			), "backfill", at(200)),
			params: verdict.BackfillParams{BigJob: "big", BigPods: 4,
				BlockerJobs: []string{"blk0", "blk1", "blk2"}, SmallJobs: []string{"s0"}, ToleranceSec: 5},
			err: "blocker job blk2 never finished",
		},
		{
			name:   "not supported when the short jobs wait behind the gang",
			tl:     scenario(60.5, 61, 61, 86, 86),
			params: params,
			want:   want{outcome: verdict.NotSupported, reason: "no short job ran before the gang started"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := verdict.Backfill(tc.tl, tc.params)
			if tc.err != "" {
				require.EqualError(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			check(t, v, tc.want)
		})
	}
}
