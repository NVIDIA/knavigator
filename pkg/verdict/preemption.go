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

	"github.com/NVIDIA/knavigator/pkg/eventlog"
)

// PreemptionParams are the preemption probe's parameters: the
// high-priority Job and its pod count, the low-priority victim Job, and the
// grace after the high-priority job is scheduled.
type PreemptionParams struct {
	HighJob   string  `json:"high_job"`
	HighPods  int     `json:"high_pods"`
	VictimJob string  `json:"victim_job"`
	GraceSec  float64 `json:"grace_sec"`
}

func (p PreemptionParams) validate() error {
	if p.HighJob == "" || p.VictimJob == "" {
		return errors.New("high_job and victim_job must both be set")
	}
	if p.HighPods < 1 {
		return fmt.Errorf("high_pods %d, want 1 or more", p.HighPods)
	}
	if p.GraceSec < 0 {
		return fmt.Errorf("grace_sec %s is negative", fmtSec(p.GraceSec))
	}
	return nil
}

// Preemption judges whether making room for the high-priority job evicted
// the victim workload as a whole. The victim pods considered are those
// running when the high-priority Job was submitted: scheduled before it and
// not yet ended. Pods that ended earlier, and pods the victim's controller
// recreates later, are not. A victim pod is left running when it
// had not ended GraceSec after the high-priority job's last pod was
// scheduled. When the window ends before that, a victim pod not yet ended
// cannot be judged, and Preemption returns an error instead of a verdict.
func Preemption(t Timeline, p PreemptionParams) (Verdict, error) {
	if err := p.validate(); err != nil {
		return Verdict{}, err
	}
	high, seen := t.job(p.HighJob)
	if !seen {
		return Verdict{}, fmt.Errorf("no events recorded for job %s", p.HighJob)
	}
	victims := ofJob(t.Pods, p.VictimJob)
	highCreated := t.submitted(p.HighJob)
	if highCreated.IsZero() {
		return Verdict{}, fmt.Errorf("no submission recorded for job %s", p.HighJob)
	}
	var before []*Pod
	for _, q := range victims {
		if !q.Scheduled.IsZero() && q.Scheduled.Before(highCreated) && (q.End.IsZero() || q.End.After(highCreated)) {
			before = append(before, q)
		}
	}
	if len(before) == 0 {
		return Verdict{}, errors.New("no victim pod was running when the high-priority job was created")
	}
	v := Verdict{
		Measurements: map[string]float64{"victim_pods_before": float64(len(before))},
		EvidenceSeq:  evidence(high, victims, t.jobSeqs(p.HighJob, p.VictimJob)),
	}
	th, ok := nthScheduled(high, p.HighPods)
	if !ok {
		v.Outcome = NotSupported
		v.Reason = fmt.Sprintf("the high-priority job did not get all %d pods scheduled", p.HighPods)
		return v, nil
	}
	cut := th.Add(seconds(p.GraceSec))
	if t.End.Before(cut) {
		// a victim not ended by the window's end may yet end before the cut
		for _, q := range before {
			if q.End.IsZero() {
				return Verdict{}, fmt.Errorf("the window ends %s s after the high-priority job was scheduled, before the %s s grace, and victim pod %s has no recorded end",
					fmtSec(t.End.Sub(th).Seconds()), fmtSec(p.GraceSec), q.Name)
			}
		}
	}
	remaining, evicted, lost := 0, 0, 0.0
	for _, q := range before {
		if !q.End.IsZero() && !q.End.After(cut) {
			if q.EndBy == eventlog.Succeeded {
				// an error, not a verdict: the victims must outlive the cut
				return Verdict{}, fmt.Errorf("victim pod %s completed instead of being evicted, so the probe did not exercise preemption", q.Name)
			}
			evicted++
			if !q.Running.IsZero() {
				// rounded before the sum, so no architecture fuses it (fairshare.go)
				lost += float64(float64(q.GPUs) * q.End.Sub(q.Running).Seconds())
			}
			continue
		}
		remaining++
	}
	v.Measurements["high_time_to_schedule_s"] = th.Sub(highCreated).Seconds()
	v.Measurements["victim_pods_evicted"] = float64(evicted)
	v.Measurements["victim_pods_remaining"] = float64(remaining)
	v.Measurements["gpu_seconds_lost"] = lost
	if remaining == 0 {
		v.Outcome = Supported
		v.Reason = fmt.Sprintf("no victim pod was left running %s s after the high-priority job was scheduled", fmtSec(p.GraceSec))
	} else {
		v.Outcome = Partial
		v.Reason = fmt.Sprintf("%d of %d victim pods were left running %s s after the high-priority job was scheduled", remaining, len(before), fmtSec(p.GraceSec))
	}
	return v, nil
}
