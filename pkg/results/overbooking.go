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
	"fmt"
	"math"
	"sort"
	"time"
)

// Overbooking reports GPUs bound beyond a node's allocatable count. A scheduler that counts a
// node's GPUs wrongly, such as once as device-plugin resources and again as DRA devices, can bind
// more GPU pods than the node holds, and then every other metric of the run is wrong too.
type Overbooking struct {
	// OverbookedNodes is how many nodes ever held more GPUs than they have.
	OverbookedNodes int `json:"overbooked_nodes"`
	// OverbookedPeakGPUs is the largest excess on one node at one time.
	OverbookedPeakGPUs float64 `json:"overbooked_peak_gpus"`
	// OverbookedGPUHours is the GPU time bound beyond the nodes' allocatable GPUs.
	OverbookedGPUHours float64 `json:"overbooked_gpu_hours"`
}

// overbooking sweeps each node's bound GPUs, counting GPUs that pods from before the run hold there,
// against its allocatable count. Pods still running at the end count until end.
func overbooking(pods []Pod, nodes, held map[string]float64, end time.Time) Overbooking {
	type change struct {
		t    time.Time
		gpus float64
	}
	byNode := map[string][]change{}
	for _, p := range pods {
		if p.Scheduled.IsZero() || p.Node == "" || p.GPUs <= 0 {
			continue
		}
		if _, ok := nodes[p.Node]; !ok {
			continue
		}
		fin := p.Finished
		if fin.IsZero() {
			fin = end
		}
		if fin.After(p.Scheduled) {
			byNode[p.Node] = append(byNode[p.Node], change{p.Scheduled, p.GPUs}, change{fin, -p.GPUs})
		}
	}
	var out Overbooking
	for node, changes := range byNode {
		// Times are whole seconds, so a pod that ends and one that starts in the same second are a
		// handover, not an overlap: apply releases first.
		sort.Slice(changes, func(a, b int) bool {
			if !changes[a].t.Equal(changes[b].t) {
				return changes[a].t.Before(changes[b].t)
			}
			return changes[a].gpus < changes[b].gpus
		})
		used, over := held[node], false
		for i, c := range changes {
			used += c.gpus
			excess := used - nodes[node]
			if excess <= 0 {
				continue
			}
			over = true
			out.OverbookedPeakGPUs = math.Max(out.OverbookedPeakGPUs, excess)
			if i+1 < len(changes) {
				out.OverbookedGPUHours += excess * changes[i+1].t.Sub(c.t).Hours()
			}
		}
		if over {
			out.OverbookedNodes++
		}
	}
	return out
}

// check fails a run that bound more GPUs to a node than it has.
func (o Overbooking) check() error {
	if o.OverbookedNodes == 0 {
		return nil
	}
	return fmt.Errorf("%d nodes held more GPUs than they have (peak excess %g GPUs, %.1f GPU-hours): the scheduler counted GPUs wrongly, so the run's metrics cannot be trusted",
		o.OverbookedNodes, o.OverbookedPeakGPUs, o.OverbookedGPUHours)
}
