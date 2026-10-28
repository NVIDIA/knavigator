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
	"math"
	"sort"
	"time"
)

// Fragmentation shows where the free GPUs were; large jobs cannot use those on partly used nodes.
type Fragmentation struct {
	// FragmentedGPUHours is free GPU time on nodes that were partly in use.
	FragmentedGPUHours float64 `json:"fragmented_gpu_hours"`
	// GPUFragmentation is FragmentedGPUHours over all free GPU time; null when no GPU was free.
	GPUFragmentation *float64 `json:"gpu_fragmentation"`
	// FreeNodesMean is the time-weighted mean number of nodes with every GPU free.
	FreeNodesMean float64 `json:"free_nodes_mean"`
}

// fragmentation sweeps the window from first to last. nodes maps each node to its allocatable GPUs;
// pods on a node missing from it are left out.
func fragmentation(pods []Pod, nodes map[string]float64, first, last time.Time) Fragmentation {
	var out Fragmentation
	if !last.After(first) || len(nodes) == 0 {
		return out
	}
	type change struct {
		t    time.Time
		node string
		gpus float64
	}
	var changes []change
	for _, p := range pods {
		if p.Scheduled.IsZero() || p.Node == "" {
			continue
		}
		if _, ok := nodes[p.Node]; !ok {
			continue
		}
		fin := p.Finished
		if fin.IsZero() || fin.After(last) {
			fin = last
		}
		if fin.After(p.Scheduled) {
			changes = append(changes, change{p.Scheduled, p.Node, p.GPUs}, change{fin, p.Node, -p.GPUs})
		}
	}
	sort.Slice(changes, func(a, b int) bool { return changes[a].t.Before(changes[b].t) })

	// Running totals over all nodes: free GPUs, free GPUs on partly used nodes, and nodes with
	// every GPU free. A node adds its share again after each change.
	used := map[string]float64{}
	var free, fragmented float64
	freeNodes := 0
	share := func(node string, sign float64) {
		if nodes[node] <= 0 {
			return
		}
		f := math.Max(nodes[node]-used[node], 0)
		free += sign * f
		switch {
		case f >= nodes[node]:
			freeNodes += int(sign)
		case f > 0:
			fragmented += sign * f
		}
	}
	for node := range nodes {
		share(node, 1)
	}

	var freeSec, fragmentedSec, freeNodeSec float64
	prev := first
	hold := func(until time.Time) {
		if dt := until.Sub(prev).Seconds(); dt > 0 {
			freeSec += free * dt
			fragmentedSec += fragmented * dt
			freeNodeSec += float64(freeNodes) * dt
			prev = until
		}
	}
	for _, c := range changes {
		hold(c.t)
		share(c.node, -1)
		used[c.node] += c.gpus
		share(c.node, 1)
	}
	hold(last)

	out.FragmentedGPUHours = fragmentedSec / 3600
	if freeSec > 0 {
		f := fragmentedSec / freeSec
		out.GPUFragmentation = &f
	}
	out.FreeNodesMean = freeNodeSec / last.Sub(first).Seconds()
	return out
}
