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
	require.NotNil(t, f.GPUFragmentationLargestPod)
	require.InDelta(t, 5.0/13, *f.GPUFragmentationLargestPod, 1e-15, "150 GPU-seconds on nodes with fewer than 5 free")
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
	require.NotNil(t, idle.GPUFragmentationLargestPod)
	require.Zero(t, *idle.GPUFragmentationLargestPod, "no pods, so no free GPU is too small")
	require.Equal(t, 2.0, idle.FreeNodesMean)

	full := fragmentation([]Pod{
		{Node: "n0", GPUs: 8, Scheduled: at(0), Finished: at(10)},
		{Node: "n1", GPUs: 8, Scheduled: at(0), Finished: at(10)},
	}, nodes, at(0), at(10))
	require.Nil(t, full.GPUFragmentation, "no free GPU, so the share is undefined")
	require.Nil(t, full.GPUFragmentationLargestPod)
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
	require.NotNil(t, s.GPUFragmentationLargestPod)
	require.Zero(t, *s.GPUFragmentationLargestPod, "n0's 4 free GPUs still fit the largest pod")
	require.Equal(t, 1.0, s.FreeNodesMean)

	s.ScaleTime(10)
	require.InDelta(t, 4000.0/3600, s.FragmentedGPUHours, 1e-12)
	require.InDelta(t, 1.0/3, *s.GPUFragmentation, 1e-12, "a share does not scale")
	require.Zero(t, *s.GPUFragmentationLargestPod)

	data, err := json.Marshal(s)
	require.NoError(t, err)
	var tree interface{}
	require.NoError(t, json.Unmarshal(data, &tree))
	require.InDelta(t, 1.0/3, values(samples(tree))["knavigator_gpu_fragmentation"], 1e-12)
	require.Contains(t, values(samples(tree)), "knavigator_gpu_fragmentation_largest_pod")

	data, err = json.Marshal(Summary{})
	require.NoError(t, err)
	require.Contains(t, string(data), `"gpu_fragmentation":null`)
	require.NoError(t, json.Unmarshal(data, &tree))
	require.NotContains(t, values(samples(tree)), "knavigator_gpu_fragmentation", "null is not pushed")
}

func TestStrandedGPUHours(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	nodes := map[string]float64{"n0": 8, "n1": 4}
	// The 8-GPU pod strands 2+4 free GPUs until 50s, when a 4-GPU pod that fits n1 starts waiting.
	pods := []Pod{
		{Node: "n0", GPUs: 6, Created: at(0), Scheduled: at(0), Finished: at(100)},
		{GPUs: 8, Created: at(0), Finished: at(100), Phase: phaseDeleted},
		{Node: "n1", GPUs: 4, Created: at(50), Scheduled: at(60)},
	}
	f := fragmentation(pods, nodes, at(0), at(100))
	require.InDelta(t, (6*50.0+2*10+2*40)/3600, f.StrandedGPUHours, 1e-12)

	idle := fragmentation(pods[:1], nodes, at(0), at(100))
	require.Zero(t, idle.StrandedGPUHours, "nothing waits")

	s := Summarize(pods, nil, 12, at(100), Options{NodeGPUs: nodes})
	s.ScaleTime(10)
	require.InDelta(t, 10*400.0/3600, s.StrandedGPUHours, 1e-12)
}

func TestRackFragmentation(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	nodes := map[string]float64{"n0": 8, "n1": 8, "n2": 8, "n3": 8, "n4": 8}
	racks := map[string]string{"n0": "a", "n1": "a", "n2": "b", "n3": "b"}
	// One pod fills n0 to the end; no node is partly used, but rack a is, so its other 8 GPUs count.
	pods := []Pod{{Node: "n0", GPUs: 8, Created: at(0), Scheduled: at(0)}}

	s := Summarize(pods, nil, 40, at(100), Options{NodeGPUs: nodes, NodeRacks: racks})
	require.NotNil(t, s.GPUFragmentation)
	require.Zero(t, *s.GPUFragmentation)
	require.NotNil(t, s.GPUFragmentationRack)
	require.InDelta(t, 8.0/24, *s.GPUFragmentationRack, 1e-12, "n4 has no rack and is left out")
	more := append([]Pod{{Node: "n4", GPUs: 8, Scheduled: at(0)}, {Node: "n2", GPUs: 8, Scheduled: at(50), Finished: at(40)}}, pods...)
	f := rackFragmentation(more, nodes, nil, racks, at(0), at(100))
	require.NotNil(t, f)
	require.InDelta(t, 8.0/24, *f, 1e-12, "neither a pod on n4 nor one that ends before it is placed changes it")

	require.Nil(t, Summarize(pods, nil, 40, at(100), Options{NodeGPUs: nodes}).GPUFragmentationRack)
	full := []Pod{{Node: "n0", GPUs: 8, Scheduled: at(0), Finished: at(100)}, {Node: "n1", GPUs: 8, Scheduled: at(0), Finished: at(100)}}
	require.Nil(t, rackFragmentation(full, nodes, nil, map[string]string{"n0": "a", "n1": "a"}, at(0), at(100)), "no free GPU")

	// A pod from before the run holds n2 for the whole window, so rack b is in use too.
	s = Summarize(pods, nil, 32, at(100), Options{NodeGPUs: nodes, NodeHeldGPUs: map[string]float64{"n2": 8}, NodeRacks: racks})
	require.NotNil(t, s.GPUFragmentationRack)
	require.InDelta(t, 1.0, *s.GPUFragmentationRack, 1e-12)

	// A finished leftover still holds n2 and a run pod takes it too, so n2 adds no free GPUs.
	onHeld := []Pod{{Node: "n2", GPUs: 8, Created: at(0), Scheduled: at(0)}}
	f = rackFragmentation(onHeld, nodes, map[string]float64{"n2": 8}, racks, at(0), at(100))
	require.NotNil(t, f)
	require.InDelta(t, 8.0/24, *f, 1e-12)

	// The pod holds n0 for the first 25s only: 8*25 of 8*25 + 16*75 + 16*100 free GPU-seconds.
	short := []Pod{{Node: "n0", GPUs: 8, Created: at(0), Scheduled: at(0), Finished: at(25)}}
	f = rackFragmentation(short, nodes, nil, racks, at(0), at(100))
	require.NotNil(t, f)
	require.InDelta(t, 200.0/3000, *f, 1e-12)
}

func TestNodesByFreeGPUs(t *testing.T) {
	// The golden fleet: n0 stays empty, n1 has 3 free for 10s, n3 none for 10s, over 20s.
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	nodes := map[string]float64{"n0": 8, "n1": 8, "n2": 4, "n3": 4, "cpu": 0}
	pods := []Pod{
		{Node: "n1", GPUs: 5, Scheduled: at(0), Finished: at(10)},
		{Node: "n3", GPUs: 4, Scheduled: at(0), Finished: at(10)},
		{Node: "gone", GPUs: 8, Scheduled: at(0), Finished: at(20)},
	}
	f := fragmentation(pods, nodes, at(0), at(20))

	byFree := func(s NodeSize) map[int]float64 {
		out := map[int]float64{}
		for _, c := range s.ByFree {
			out[c.FreeGPUs] = c.NodesMean
		}
		return out
	}
	require.Len(t, f.NodesByFreeGPUs, 2, "the zero-GPU node is left out")
	eight, four := f.NodesByFreeGPUs[0], f.NodesByFreeGPUs[1]
	require.Equal(t, 8, eight.NodeGPUs)
	require.Equal(t, 2, eight.Nodes)
	require.Len(t, eight.ByFree, 9)
	require.Equal(t, 8, eight.ByFree[0].FreeGPUs, "all free first")
	require.Equal(t, 0, eight.ByFree[8].FreeGPUs)
	require.Equal(t, map[int]float64{8: 1.5, 7: 0, 6: 0, 5: 0, 4: 0, 3: 0.5, 2: 0, 1: 0, 0: 0}, byFree(eight))
	require.Equal(t, 4, four.NodeGPUs)
	require.Equal(t, map[int]float64{4: 1.5, 3: 0, 2: 0, 1: 0, 0: 0.5}, byFree(four))

	// The all-free counts give free_nodes_mean, and weighted by free GPUs, gpu_fragmentation.
	require.InDelta(t, f.FreeNodesMean, byFree(eight)[8]+byFree(four)[4], 1e-12)
	var free, fragmented float64
	for _, s := range f.NodesByFreeGPUs {
		for _, c := range s.ByFree {
			free += float64(c.FreeGPUs) * c.NodesMean
			if c.FreeGPUs < s.NodeGPUs {
				fragmented += float64(c.FreeGPUs) * c.NodesMean
			}
		}
	}
	require.InDelta(t, *f.GPUFragmentation, fragmented/free, 1e-12)

	// Held GPUs count as in use for the whole window, and a pod past the end stops at the end.
	held := fragmentationHeld([]Pod{{Node: "n1", GPUs: 8, Scheduled: at(5), Finished: at(30)}},
		map[string]float64{"n0": 8, "n1": 8}, map[string]float64{"n0": 2}, at(0), at(10))
	require.Equal(t, map[int]float64{8: 0.5, 7: 0, 6: 1, 5: 0, 4: 0, 3: 0, 2: 0, 1: 0, 0: 0.5},
		byFree(held.NodesByFreeGPUs[0]))

	// A leftover holding a whole node plus a run pod on it reads 0 free, not below.
	over := fragmentationHeld([]Pod{{Node: "n0", GPUs: 4, Scheduled: at(0), Finished: at(10)}},
		map[string]float64{"n0": 8}, map[string]float64{"n0": 8}, at(0), at(10))
	require.Equal(t, 1.0, byFree(over.NodesByFreeGPUs[0])[0])

	// A placement read before the window start counts from the start, not before it.
	early := Summarize([]Pod{{Node: "n0", GPUs: 8, Created: at(10), Scheduled: at(5), Finished: at(20)}},
		nil, 8, at(20), Options{NodeGPUs: map[string]float64{"n0": 8}})
	require.Equal(t, map[int]float64{8: 0, 7: 0, 6: 0, 5: 0, 4: 0, 3: 0, 2: 0, 1: 0, 0: 1},
		byFree(early.NodesByFreeGPUs[0]))
	require.Equal(t, early.FreeNodesMean, byFree(early.NodesByFreeGPUs[0])[8])

	// Nodes ending at different sub-second times give the same means however the map iterates.
	end := at(10)
	ragged := []Pod{
		{Node: "a", GPUs: 8, Scheduled: at(0), Finished: end.Add(-100 * time.Millisecond)},
		{Node: "b", GPUs: 8, Scheduled: at(0), Finished: end.Add(-200 * time.Millisecond)},
		{Node: "c", GPUs: 8, Scheduled: at(0), Finished: end.Add(-300 * time.Millisecond)},
	}
	three := map[string]float64{"a": 8, "b": 8, "c": 8}
	want := fragmentation(ragged, three, at(0), end).NodesByFreeGPUs
	for range 200 {
		require.Equal(t, want, fragmentation(ragged, three, at(0), end).NodesByFreeGPUs)
	}

	// A handover in the same second leaves no time at the in-between count.
	handover := fragmentation([]Pod{
		{Node: "n0", GPUs: 8, Scheduled: at(0), Finished: at(5)},
		{Node: "n0", GPUs: 8, Scheduled: at(5), Finished: at(10)},
	}, map[string]float64{"n0": 8}, at(0), at(10))
	require.Equal(t, 1.0, byFree(handover.NodesByFreeGPUs[0])[0])

	// Sizes come largest first however the map iterates.
	mixed := map[string]float64{"a": 1, "b": 2, "c": 4, "d": 6, "e": 8, "f": 16}
	for range 20 {
		var got []int
		for _, s := range fragmentation(nil, mixed, at(0), at(10)).NodesByFreeGPUs {
			got = append(got, s.NodeGPUs)
		}
		require.Equal(t, []int{16, 8, 6, 4, 2, 1}, got)
	}

	require.Nil(t, fragmentation(pods, nodes, at(0), at(0)).NodesByFreeGPUs, "no time, no field")
	require.Nil(t, fragmentation(nil, map[string]float64{"cpu": 0}, at(0), at(10)).NodesByFreeGPUs)
}

func TestNodesByFreeGPUsOutput(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := t0.Add(10 * time.Second)
	nodes := map[string]float64{"a": 2, "b": 2, "c": 1}
	pods := []Pod{{Node: "a", GPUs: 1, Created: t0, Scheduled: t0, Finished: end}}
	s := Summarize(pods, nil, 5, end, Options{NodeGPUs: nodes})
	s.ScaleTime(10)

	data, err := json.Marshal(s)
	require.NoError(t, err)
	require.Contains(t, string(data), `"nodes_by_free_gpus":[{"node_gpus":2,"nodes":2,"by_free":[`+
		`{"free_gpus":2,"nodes_mean":1},{"free_gpus":1,"nodes_mean":1},{"free_gpus":0,"nodes_mean":0}]},`+
		`{"node_gpus":1,"nodes":1,"by_free":[{"free_gpus":1,"nodes_mean":1},{"free_gpus":0,"nodes_mean":0}]}]`,
		"rows are in a stable order, and means are not scaled by -speedup")

	var tree interface{}
	require.NoError(t, json.Unmarshal(data, &tree))
	got := values(samples(tree))
	require.Equal(t, 1.0, got["knavigator_nodes_by_free_gpus_by_free_nodes_mean,node_gpus=2,free_gpus=1"])
	require.Equal(t, 0.0, got["knavigator_nodes_by_free_gpus_by_free_nodes_mean,node_gpus=1,free_gpus=0"])
	require.Equal(t, 2.0, got["knavigator_nodes_by_free_gpus_nodes,node_gpus=2"])
	require.NotContains(t, got, "knavigator_nodes_by_free_gpus_by_free_free_gpus,node_gpus=2,free_gpus=1",
		"the label field is not pushed as a value")

	data, err = json.Marshal(Summary{})
	require.NoError(t, err)
	require.NotContains(t, string(data), "nodes_by_free_gpus")
}
