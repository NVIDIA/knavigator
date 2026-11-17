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

func TestOverbooking(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	nodes := map[string]float64{"n1": 8, "n2": 8}

	// One pod hands its GPUs to the next in the same second: not an overlap.
	handover := []Pod{
		{Node: "n1", GPUs: 8, Scheduled: at(0), Finished: at(10)},
		{Node: "n1", GPUs: 8, Scheduled: at(10), Finished: at(20)},
	}
	require.Equal(t, Overbooking{}, overbooking(handover, nodes, nil, at(20)))
	require.NoError(t, overbooking(handover, nodes, nil, at(20)).check())

	// Two 8-GPU pods share an 8-GPU node for 3600s, as when a scheduler counts each GPU twice.
	double := []Pod{
		{Node: "n1", GPUs: 8, Scheduled: at(0), Finished: at(7200)},
		{Node: "n1", GPUs: 8, Scheduled: at(3600), Finished: at(7200)},
		{Node: "n2", GPUs: 8, Scheduled: at(0), Finished: at(7200)},
	}
	o := overbooking(double, nodes, nil, at(7200))
	require.Equal(t, Overbooking{OverbookedNodes: 1, OverbookedPeakGPUs: 8, OverbookedGPUHours: 8}, o)
	require.ErrorContains(t, o.check(), "1 nodes held more GPUs than they have")

	// GPUs that pods from before the run hold count against the node.
	o = overbooking([]Pod{{Node: "n2", GPUs: 6, Scheduled: at(0), Finished: at(3600)}}, nodes, map[string]float64{"n2": 4}, at(3600))
	require.Equal(t, Overbooking{OverbookedNodes: 1, OverbookedPeakGPUs: 2, OverbookedGPUHours: 2}, o)

	// A pod still running at the end counts until the end; unknown nodes and unplaced pods are skipped.
	running := []Pod{
		{Node: "n1", GPUs: 8, Scheduled: at(0)},
		{Node: "n1", GPUs: 4, Scheduled: at(1800), Finished: at(3600)},
		{Node: "other", GPUs: 64, Scheduled: at(0), Finished: at(3600)},
		{GPUs: 8, Created: at(0)},
	}
	o = overbooking(running, nodes, nil, at(7200))
	require.Equal(t, Overbooking{OverbookedNodes: 1, OverbookedPeakGPUs: 4, OverbookedGPUHours: 2}, o)
}

func TestSummarizeReportsOverbooking(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pods := []Pod{
		{Name: "a", Node: "n1", GPUs: 8, Created: t0, Scheduled: t0, Finished: t0.Add(time.Hour)},
		{Name: "b", Node: "n1", GPUs: 8, Created: t0, Scheduled: t0, Finished: t0.Add(time.Hour)},
	}
	s := Summarize(pods, nil, 8, t0.Add(time.Hour), Options{NodeGPUs: map[string]float64{"n1": 8}})
	require.Equal(t, 1, s.OverbookedNodes)
	s.ScaleTime(60)
	require.InDelta(t, 480, s.OverbookedGPUHours, 1e-9)
}
