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

package verdict

import (
	"errors"
	"fmt"
)

// PriorityParams are the priority probe's parameters: the high-priority
// Job, and the lower-priority Jobs queued before it arrived.
type PriorityParams struct {
	HighJob    string   `json:"high_job"`
	QueuedJobs []string `json:"queued_jobs"`
}

func (p PriorityParams) validate() error {
	if p.HighJob == "" {
		return errors.New("high_job is empty")
	}
	if len(p.QueuedJobs) == 0 {
		return errors.New("queued_jobs is empty")
	}
	return nil
}

// Priority judges whether the high-priority job was scheduled ahead of the
// queued lower-priority jobs. A queued job counts as ahead only when its
// first pod was scheduled strictly before the high-priority job's first pod,
// and counts once however many pods it has. Time to schedule runs
// from the high-priority Job's submission, so a queue that holds the job
// before creating its pods is measured too.
func Priority(t Timeline, p PriorityParams) (Verdict, error) {
	if err := p.validate(); err != nil {
		return Verdict{}, err
	}
	high, seen := t.job(p.HighJob)
	if !seen {
		return Verdict{}, fmt.Errorf("no events recorded for job %s", p.HighJob)
	}
	var queued []*Pod
	var byJob [][]*Pod
	for _, j := range p.QueuedJobs {
		ps, seen := t.job(j)
		if !seen {
			return Verdict{}, fmt.Errorf("no events recorded for job %s", j)
		}
		queued = append(queued, ps...)
		byJob = append(byJob, ps)
	}
	submitted := t.submitted(p.HighJob)
	if submitted.IsZero() {
		return Verdict{}, fmt.Errorf("no submission recorded for job %s", p.HighJob)
	}
	v := Verdict{
		Measurements: map[string]float64{"queued_jobs": float64(len(p.QueuedJobs))},
		EvidenceSeq:  evidence(high, queued, t.jobSeqs(append([]string{p.HighJob}, p.QueuedJobs...)...)),
	}
	hs, ok := nthScheduled(high, 1)
	// a queued job is ahead when its first pod was scheduled strictly first
	before := 0
	for _, ps := range byJob {
		if first, sched := nthScheduled(ps, 1); sched && (!ok || first.Before(hs)) {
			before++
		}
	}
	v.Measurements["queued_before_high"] = float64(before)
	if !ok {
		v.Outcome = NotSupported
		v.Reason = "the high-priority job was never scheduled"
		return v, nil
	}
	v.Measurements["high_time_to_schedule_s"] = hs.Sub(submitted).Seconds()
	switch before {
	case 0:
		v.Outcome = Supported
		v.Reason = "the high-priority job was scheduled before every queued lower-priority job"
	case len(p.QueuedJobs):
		v.Outcome = NotSupported
		v.Reason = fmt.Sprintf("all %d queued lower-priority jobs were scheduled before the high-priority job", len(p.QueuedJobs))
	default:
		v.Outcome = Partial
		v.Reason = fmt.Sprintf("%d of %d queued lower-priority jobs were scheduled before the high-priority job", before, len(p.QueuedJobs))
	}
	return v, nil
}
