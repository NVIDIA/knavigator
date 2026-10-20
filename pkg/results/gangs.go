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

	corev1 "k8s.io/api/core/v1"
)

// GangSize summarizes the jobs that have this many pods.
type GangSize struct {
	// Pods is the number of pods per job, the gang size.
	Pods      int `json:"gang_size"`
	Submitted int `json:"submitted"`
	// Waiting jobs have at least one pod not yet placed, including gangs that are partly placed.
	Waiting int `json:"waiting"`
	// Running jobs have every pod placed and at least one pod still running.
	Running int `json:"running"`
	// Complete jobs have every pod finished.
	Complete int `json:"complete"`
	// WaitSec is the wait of jobs that have started, as in Summary.JobWaitSec.
	WaitSec Stats `json:"wait_sec"`
}

// byGangSize counts jobs by pod count. placed maps job name to the earliest placement per pod name.
func byGangSize(pods []Pod, jobs []Job, placed map[string]map[string]time.Time) []GangSize {
	byJob := map[string][]Pod{}
	for _, p := range pods {
		byJob[p.Job] = append(byJob[p.Job], p)
	}
	sizes := map[int]*GangSize{}
	waits := map[int][]float64{}
	for _, j := range jobs {
		if j.Pods == 0 {
			continue
		}
		g := sizes[j.Pods]
		if g == nil {
			g = &GangSize{Pods: j.Pods}
			sizes[j.Pods] = g
		}
		g.Submitted++
		switch {
		case len(placed[j.Name]) < j.Pods:
			g.Waiting++
		case succeeded(byJob[j.Name]) >= j.Pods:
			g.Complete++
			waits[j.Pods] = append(waits[j.Pods], startWait(placed[j.Name], j))
		default:
			g.Running++
			waits[j.Pods] = append(waits[j.Pods], startWait(placed[j.Name], j))
		}
	}

	out := make([]GangSize, 0, len(sizes))
	for n, g := range sizes {
		g.WaitSec = stats(waits[n])
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pods < out[j].Pods })
	return out
}

// startWait is the time from submission until the job's last pod was placed.
func startWait(placed map[string]time.Time, j Job) float64 {
	ts := make([]time.Time, 0, len(placed))
	for _, t := range placed {
		ts = append(ts, t)
	}
	sort.Slice(ts, func(a, b int) bool { return ts[a].Before(ts[b]) })
	// Pod timestamps have one second resolution, so a fast placement can read as before submission.
	return math.Max(0, ts[j.Pods-1].Sub(j.Submitted).Seconds())
}

// succeeded counts the pods of a job that finished successfully. A pod that was evicted, or that
// ended without being placed, is a lost attempt, and its replacement is a different pod.
func succeeded(pods []Pod) int {
	var n int
	for _, p := range pods {
		if p.Phase == string(corev1.PodSucceeded) {
			n++
		}
	}
	return n
}
