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

// idleWaitingGPUHours integrates, over the run, the smaller of the idle GPUs and the GPUs that pods
// are waiting for: capacity that stayed unused while work could not be placed. A pod waits from its
// creation until it is placed, or until it ends if it never was.
func idleWaitingGPUHours(pods []Pod, capacity float64, end time.Time) float64 {
	if capacity <= 0 {
		return 0
	}
	type step struct {
		t              time.Time
		inUse, pending float64
	}
	var steps []step
	for _, p := range pods {
		fin := p.Finished
		if fin.IsZero() {
			fin = end
		}
		placed := p.Scheduled
		if placed.IsZero() {
			placed = fin
		}
		if placed.After(p.Created) {
			steps = append(steps, step{p.Created, 0, p.GPUs}, step{placed, 0, -p.GPUs})
		}
		if !p.Scheduled.IsZero() && fin.After(p.Scheduled) {
			steps = append(steps, step{p.Scheduled, p.GPUs, 0}, step{fin, -p.GPUs, 0})
		}
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].t.Before(steps[j].t) })

	var inUse, pending, gpuSeconds float64
	var prev time.Time
	for i, s := range steps {
		if i > 0 {
			if dt := s.t.Sub(prev).Seconds(); dt > 0 {
				gpuSeconds += math.Min(math.Max(capacity-inUse, 0), pending) * dt
			}
		}
		inUse += s.inUse
		pending += s.pending
		prev = s.t
	}
	return gpuSeconds / 3600
}

// bindingGapGPUHours is GPU time that gang pods held before their gang was complete: a bound pod
// holds its GPUs, but the gang cannot start until its last pod is bound. A gang that never completed
// held its GPUs for the whole time. Evicted pods count toward completing the gang while they were
// bound, but their own time is left out, because the eviction metric already counts it.
func bindingGapGPUHours(pods []Pod, jobs []Job, end time.Time) float64 {
	byJob := map[string][]Pod{}
	for _, p := range pods {
		byJob[p.Job] = append(byJob[p.Job], p)
	}
	var gpuSeconds float64
	for _, j := range jobs {
		if j.Pods <= 1 {
			continue
		}
		// The gang is complete once it holds all its pods at once. A bind adds a member and an
		// eviction removes one; a pod that finished stays a member.
		type change struct {
			t time.Time
			d int
		}
		var changes []change
		for _, p := range byJob[j.Name] {
			if p.Scheduled.IsZero() {
				continue
			}
			changes = append(changes, change{p.Scheduled, 1})
			if p.Phase == phaseDeleted && !p.Finished.IsZero() {
				changes = append(changes, change{p.Finished, -1})
			}
		}
		sort.Slice(changes, func(a, b int) bool {
			if !changes[a].t.Equal(changes[b].t) {
				return changes[a].t.Before(changes[b].t)
			}
			return changes[a].d < changes[b].d // an eviction at the same instant comes first
		})
		var complete time.Time // zero while the gang is short of pods
		members := 0
		for _, c := range changes {
			if members += c.d; members >= j.Pods {
				complete = c.t
				break
			}
		}
		for _, p := range byJob[j.Name] {
			if p.Scheduled.IsZero() || p.Phase == phaseDeleted {
				continue
			}
			held := p.Finished
			if held.IsZero() {
				held = end
			}
			if !complete.IsZero() && complete.Before(held) {
				held = complete
			}
			if held.After(p.Scheduled) {
				gpuSeconds += p.GPUs * held.Sub(p.Scheduled).Seconds()
			}
		}
	}
	return gpuSeconds / 3600
}
