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

import "sort"

// sloLimits is the wait ladder for slo_attainment, in simulated seconds. A fixed ladder means no
// per-class target has to be chosen before a run.
var sloLimits = []struct {
	label string
	sec   float64
}{{"1m", 60}, {"5m", 300}, {"15m", 900}, {"1h", 3600}, {"4h", 14400}}

// ClassWait summarizes the jobs of one class.
type ClassWait struct {
	Class   string `json:"class"`
	Jobs    int    `json:"jobs"`
	Started int    `json:"started"`
	WaitSec Stats  `json:"wait_sec"`
	// SLOAttainment maps each limit in the ladder to the share of the class's jobs that started
	// within it. Jobs that never started count as misses.
	SLOAttainment map[string]float64 `json:"slo_attainment"`
}

// byClass groups jobs by class. waitOf holds the real-time wait of each started job.
func byClass(jobs []Job, waitOf map[string]float64, speedup float64) []ClassWait {
	speedup = max(speedup, 1)
	rows := map[string]*ClassWait{}
	waits := map[string][]float64{}
	for _, j := range jobs {
		if j.Class == "" {
			continue
		}
		r := rows[j.Class]
		if r == nil {
			r = &ClassWait{Class: j.Class, SLOAttainment: map[string]float64{}}
			rows[j.Class] = r
		}
		r.Jobs++
		w, ok := waitOf[j.Name]
		if !ok {
			continue
		}
		r.Started++
		waits[j.Class] = append(waits[j.Class], w)
		for _, l := range sloLimits {
			if w*speedup <= l.sec {
				r.SLOAttainment[l.label]++
			}
		}
	}

	out := make([]ClassWait, 0, len(rows))
	for class, r := range rows {
		r.WaitSec = stats(waits[class])
		for _, l := range sloLimits {
			r.SLOAttainment[l.label] /= float64(r.Jobs)
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Class < out[b].Class })
	return out
}
