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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFragmentationGolden(t *testing.T) {
	// The lab's node-level fragmentation vector: 390 free GPU-seconds over 20s, 30 on partly used nodes.
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	nodes := map[string]float64{"n0": 8, "n1": 8, "n2": 4, "n3": 4}
	pods := []Pod{
		{Node: "n1", GPUs: 5, Created: at(0), Scheduled: at(0), Finished: at(10)},
		{Node: "n3", GPUs: 4, Created: at(0), Scheduled: at(0), Finished: at(10)},
	}
	f := fragmentation(pods, nodes, at(0), at(20))

	require.InDelta(t, 30.0/3600, f.FragmentedGPUHours, 1e-15)
	require.NotNil(t, f.GPUFragmentation)
	require.InDelta(t, 1.0/13, *f.GPUFragmentation, 1e-15)
	require.InDelta(t, 3.0, f.FreeNodesMean, 1e-12)
}

func TestFragmentationEdges(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	nodes := map[string]float64{"n0": 8, "n1": 8}

	idle := fragmentation(nil, nodes, at(0), at(10))
	require.Zero(t, idle.FragmentedGPUHours)
	require.NotNil(t, idle.GPUFragmentation)
	require.Zero(t, *idle.GPUFragmentation)
	require.Equal(t, 2.0, idle.FreeNodesMean)

	full := fragmentation([]Pod{
		{Node: "n0", GPUs: 8, Scheduled: at(0), Finished: at(10)},
		{Node: "n1", GPUs: 8, Scheduled: at(0), Finished: at(10)},
	}, nodes, at(0), at(10))
	require.Nil(t, full.GPUFragmentation, "no free GPU, so the share is undefined")
	require.Zero(t, full.FreeNodesMean)

	// A handover in the same second never leaves both nodes free.
	handover := fragmentation([]Pod{
		{Node: "n1", GPUs: 8, Scheduled: at(5), Finished: at(10)},
		{Node: "n0", GPUs: 8, Scheduled: at(0), Finished: at(5), Phase: phaseDeleted},
		{GPUs: 8},
		{Node: "gone", GPUs: 8, Scheduled: at(0), Finished: at(10)},
	}, nodes, at(0), at(10))
	require.Equal(t, 1.0, handover.FreeNodesMean)
	require.NotNil(t, handover.GPUFragmentation)
	require.Zero(t, *handover.GPUFragmentation)

	none := fragmentation([]Pod{{Node: "n0", GPUs: 1, Scheduled: at(0)}}, nodes, at(0), at(0))
	require.Nil(t, none.GPUFragmentation)
	require.Zero(t, none.FreeNodesMean)
}

func TestFragmentationOverMakespan(t *testing.T) {
	// A pod still running at end holds half of n0; only n0's 4x100 free GPU-seconds are fragmented.
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := t0.Add(100 * time.Second)
	nodes := map[string]float64{"n0": 8, "n1": 8}
	skewed := fragmentation([]Pod{{Node: "n0", GPUs: 4, Scheduled: t0, Finished: end.Add(time.Hour)}}, nodes, t0, end)
	require.InDelta(t, 400.0/3600, skewed.FragmentedGPUHours, 1e-12, "a finish past end counts up to end")

	pods := []Pod{{Node: "n0", GPUs: 4, Created: t0, Scheduled: t0}}
	s := Summarize(pods, nil, 16, end, Options{NodeGPUs: nodes})
	require.Equal(t, 100.0, s.MakespanSec)
	require.InDelta(t, 400.0/3600, s.FragmentedGPUHours, 1e-12)
	require.NotNil(t, s.GPUFragmentation)
	require.InDelta(t, 1.0/3, *s.GPUFragmentation, 1e-12)
	require.Equal(t, 1.0, s.FreeNodesMean)

	s.ScaleTime(10)
	require.InDelta(t, 4000.0/3600, s.FragmentedGPUHours, 1e-12)
	require.InDelta(t, 1.0/3, *s.GPUFragmentation, 1e-12, "a share does not scale")

	data, err := json.Marshal(s)
	require.NoError(t, err)
	var tree interface{}
	require.NoError(t, json.Unmarshal(data, &tree))
	require.InDelta(t, 1.0/3, values(samples(tree))["knavigator_gpu_fragmentation"], 1e-12)

	data, err = json.Marshal(Summary{})
	require.NoError(t, err)
	require.Contains(t, string(data), `"gpu_fragmentation":null`)
	require.NoError(t, json.Unmarshal(data, &tree))
	require.NotContains(t, values(samples(tree)), "knavigator_gpu_fragmentation", "null is not pushed")
}
