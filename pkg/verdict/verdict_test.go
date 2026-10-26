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

func TestPodsIgnoresEventsAfterTheWindow(t *testing.T) {
	evs := fixture("gang", done("a0", "gang-a", "n1", 0, 1, 2, 22))
	pods := verdict.Pods(evs, "gang", at(10))
	require.Len(t, pods, 1)
	require.Equal(t, at(2), pods[0].Running)
	require.True(t, pods[0].End.IsZero())
	require.Equal(t, []int64{1, 2, 3}, pods[0].Seqs)
}

func TestPodsKeepsTheFirstTerminalTransition(t *testing.T) {
	p := done("v0", "victim", "n1", 0, 1, 2, 12)
	p.later = []step{{at: 12.5, tr: eventlog.Deleted}}
	pods := verdict.Pods(fixture("preemption", p), "preemption", at(100))
	require.Len(t, pods, 1)
	require.Equal(t, at(12), pods[0].End)
	require.Equal(t, eventlog.Succeeded, pods[0].EndBy)
	require.Len(t, pods[0].Seqs, 5)
}

func TestPodsEndsAPodAtEveryTerminalTransition(t *testing.T) {
	for _, tr := range []eventlog.Transition{
		eventlog.Succeeded, eventlog.Failed, eventlog.Deleted, eventlog.Preempted, eventlog.Evicted,
	} {
		t.Run(string(tr), func(t *testing.T) {
			p := done("v0", "victim", "n1", 0, 1, 2, 12)
			p.endBy = tr
			pods := verdict.Pods(fixture("preemption", p), "preemption", at(100))
			require.Len(t, pods, 1)
			require.Equal(t, at(12), pods[0].End)
			require.Equal(t, tr, pods[0].EndBy)
		})
	}
}

func TestPodsKeepsOnlyTheNamedProbe(t *testing.T) {
	evs := append(fixture("gang", done("a0", "gang-a", "n1", 0, 1, 2, 22)),
		fixture("priority", done("x", "low", "n1", 0, 1, 2, 3))...)
	pods := verdict.Pods(evs, "priority", at(100))
	require.Len(t, pods, 1)
	require.Equal(t, "x", pods[0].Name)
}
