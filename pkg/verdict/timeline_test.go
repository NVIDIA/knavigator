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

func TestBuildIgnoresJobEventsAfterTheWindow(t *testing.T) {
	// seq 1 is the Job's creation at 0 s, seq 6 its end at 30 s
	evs := fixtureJobs("p", []job{{name: "j", tenant: "t1", gpus: 8, created: 0, end: 30}},
		done("j-0", "j", "n1", 0, 1, 2, 20))
	tl := verdict.Build(evs, "p", at(25))
	require.Contains(t, tl.Jobs, "j")
	require.Equal(t, at(0), tl.Jobs["j"].Created)
	require.True(t, tl.Jobs["j"].End.IsZero())
	require.Equal(t, []int64{1}, tl.Jobs["j"].Seqs)
}

func TestBuildEndsAJobAtItsEnd(t *testing.T) {
	for _, tr := range []eventlog.Transition{eventlog.Succeeded, eventlog.Failed, eventlog.Deleted} {
		t.Run(string(tr), func(t *testing.T) {
			evs := fixtureJobs("p", []job{{name: "j", tenant: "t1", gpus: 8, created: 0, end: 30}})
			require.Len(t, evs, 2)
			evs[1].Transition = tr
			tl := verdict.Build(evs, "p", at(60))
			require.Contains(t, tl.Jobs, "j")
			require.Equal(t, at(30), tl.Jobs["j"].End)
		})
	}
}

func TestBuildDerivesAJobFromItsPods(t *testing.T) {
	tests := []struct {
		name    string
		pods    []pod
		created float64
		// end is the derived Job's end; negative means it has none
		end float64
	}{
		{
			name:    "created with its first pod and ended with its last",
			pods:    []pod{done("j-0", "j", "n1", 5, 6, 7, 20), done("j-1", "j", "n2", 7, 8, 9, 25)},
			created: 5,
			end:     25,
		},
		{
			name:    "not ended while one pod still runs",
			pods:    []pod{done("j-0", "j", "n1", 5, 6, 7, 20), tenantPod("j-1", "t1", 8, 7, 9, -1)},
			created: 5,
			end:     -1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.pods {
				tc.pods[i].job = "j"
			}
			tl := verdict.Build(fixture("p", tc.pods...), "p", at(100))
			require.Contains(t, tl.Jobs, "j")
			require.Equal(t, int64(16), tl.Jobs["j"].GPUs)
			require.Equal(t, at(tc.created), tl.Jobs["j"].Created)
			if tc.end < 0 {
				require.True(t, tl.Jobs["j"].End.IsZero())
			} else {
				require.Equal(t, at(tc.end), tl.Jobs["j"].End)
			}
		})
	}
}
