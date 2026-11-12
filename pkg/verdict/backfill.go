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
	"slices"
	"time"
)

// BackfillParams are the backfill probe's parameters: the large gang, the
// blocker Jobs whose end sets the gang's earliest possible start, the short
// Jobs that may run in the gap, and the start tolerance.
type BackfillParams struct {
	BigJob       string   `json:"big_job"`
	BigPods      int      `json:"big_pods"`
	BlockerJobs  []string `json:"blocker_jobs"`
	SmallJobs    []string `json:"small_jobs"`
	ToleranceSec float64  `json:"tolerance_sec"`
}

func (p BackfillParams) validate() error {
	if p.BigJob == "" || len(p.BlockerJobs) == 0 || len(p.SmallJobs) == 0 {
		return errors.New("big_job, blocker_jobs and small_jobs must all be set")
	}
	if n := repeated(p.BlockerJobs); n != "" {
		return fmt.Errorf("blocker_jobs lists %s twice", n)
	}
	if n := repeated(p.SmallJobs); n != "" {
		return fmt.Errorf("small_jobs lists %s twice", n)
	}
	for _, n := range p.SmallJobs {
		if slices.Contains(p.BlockerJobs, n) {
			return fmt.Errorf("blocker_jobs and small_jobs both list %s", n)
		}
	}
	if slices.Contains(p.BlockerJobs, p.BigJob) {
		return fmt.Errorf("blocker_jobs lists the big_job %s", p.BigJob)
	}
	if slices.Contains(p.SmallJobs, p.BigJob) {
		return fmt.Errorf("small_jobs lists the big_job %s", p.BigJob)
	}
	if p.BigPods < 1 {
		return fmt.Errorf("big_pods %d, want 1 or more", p.BigPods)
	}
	if p.ToleranceSec < 0 {
		return fmt.Errorf("tolerance_sec %s is negative", fmtSec(p.ToleranceSec))
	}
	return nil
}

// Backfill judges whether short jobs ran in the gap before the large gang
// without delaying it. The gang starts when its BigPods-th pod is
// scheduled; its earliest possible start is the last blocker pod's end. A
// short job or gang seen only as a Job (a queue still held it) never ran.
// Only the time short jobs held nodes past the earliest start counts
// against the tolerance: a short pod scheduled before the gang started
// holds its node until it ends (capped at the gang's start), and
// short_hold_s is the longest such hold past the earliest start (0 when
// none reaches past it). The rest of the gang's delay is the scheduler's
// own start latency, recorded as big_start_after_free_s.
func Backfill(t Timeline, p BackfillParams) (Verdict, error) {
	if err := p.validate(); err != nil {
		return Verdict{}, err
	}
	big, seen := t.job(p.BigJob)
	if !seen {
		return Verdict{}, fmt.Errorf("no events recorded for job %s", p.BigJob)
	}
	blockers, missing := ofJobs(t.Pods, p.BlockerJobs)
	if missing != "" {
		return Verdict{}, fmt.Errorf("no pods recorded for blocker job %s", missing)
	}
	var smalls []*Pod
	var byJob [][]*Pod
	for _, j := range p.SmallJobs {
		ps, seen := t.job(j)
		if !seen {
			return Verdict{}, fmt.Errorf("no events recorded for job %s", j)
		}
		smalls = append(smalls, ps...)
		byJob = append(byJob, ps)
	}
	var earliest time.Time
	for _, b := range blockers {
		if b.End.IsZero() {
			return Verdict{}, fmt.Errorf("blocker job %s never finished", b.Job)
		}
		if b.End.After(earliest) {
			earliest = b.End
		}
	}
	tbig, started := nthScheduled(big, p.BigPods)
	// a short job ran first when its first pod was scheduled before the gang
	before := 0
	for _, ps := range byJob {
		if first, sched := nthScheduled(ps, 1); sched && (!started || first.Before(tbig)) {
			before++
		}
	}
	v := Verdict{
		Measurements: map[string]float64{"small_before_big": float64(before)},
		EvidenceSeq:  evidence(big, blockers, smalls, t.jobSeqs(append(append([]string{p.BigJob}, p.BlockerJobs...), p.SmallJobs...)...)),
	}
	delay, hold := 0.0, 0.0
	if started {
		delay = tbig.Sub(earliest).Seconds()
		for _, s := range smalls {
			if s.Scheduled.IsZero() || !s.Scheduled.Before(tbig) {
				continue
			}
			end := s.End
			if end.IsZero() || end.After(tbig) {
				end = tbig
			}
			if h := end.Sub(earliest).Seconds(); h > hold {
				hold = h
			}
		}
		v.Measurements["big_start_delay_s"] = delay
		v.Measurements["short_hold_s"] = hold
		v.Measurements["big_start_after_free_s"] = delay - hold
	}
	switch {
	case before == 0 && started:
		v.Outcome = NotSupported
		v.Reason = "no short job ran before the gang started"
	case before == 0:
		v.Outcome = NotSupported
		v.Reason = "no short job ran, and the gang never started"
	case !started:
		v.Outcome = Partial
		v.Reason = fmt.Sprintf("%d short jobs ran, and the gang never started", before)
	case hold > p.ToleranceSec:
		v.Outcome = Partial
		v.Reason = fmt.Sprintf("%d short jobs ran before the gang and held nodes %s s past its earliest possible start (tolerance %s s); it started %s s after that start", before, fmtSec(hold), fmtSec(p.ToleranceSec), fmtSec(delay))
	default:
		v.Outcome = Supported
		v.Reason = fmt.Sprintf("%d short jobs ran before the gang and held nodes %s s past its earliest possible start (tolerance %s s); it started %s s after that start", before, fmtSec(hold), fmtSec(p.ToleranceSec), fmtSec(delay))
	}
	return v, nil
}
