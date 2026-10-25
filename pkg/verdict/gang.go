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

// GangParams are the gang probe's parameters: the Jobs that are gangs, the
// pods per gang, and how long a gang may be partly placed before it counts.
type GangParams struct {
	Jobs     []string `json:"jobs"`
	Size     int      `json:"size"`
	GraceSec float64  `json:"grace_sec"`
}

func (p GangParams) validate() error {
	if len(p.Jobs) == 0 {
		return errors.New("jobs is empty")
	}
	if p.Size < 2 {
		return fmt.Errorf("size %d, want 2 or more", p.Size)
	}
	if p.GraceSec < 0 {
		return fmt.Errorf("grace_sec %s is negative", fmtSec(p.GraceSec))
	}
	return nil
}

// Gang judges whether gangs were placed all at once. A gang is partly
// placed from its first pod's scheduling until its Size-th pod's or, if it
// never has Size pods scheduled, until the window ends. A gang completed
// when at least Size of its pods succeeded. A gang seen only as a Job (a
// queue never admitted it) was neither placed nor completed.
func Gang(t Timeline, p GangParams) (Verdict, error) {
	if err := p.validate(); err != nil {
		return Verdict{}, err
	}
	maxPartial, worst := 0.0, ""
	completed, firstIncomplete := 0, ""
	groups := [][]*Pod{t.jobSeqs(p.Jobs...)}
	for _, job := range p.Jobs {
		ps, seen := t.job(job)
		if !seen {
			return Verdict{}, fmt.Errorf("no events recorded for job %s", job)
		}
		groups = append(groups, ps)
		if first, placed := nthScheduled(ps, 1); placed {
			last, full := nthScheduled(ps, p.Size)
			if !full {
				last = t.End
			}
			if d := last.Sub(first).Seconds(); d > maxPartial {
				maxPartial, worst = d, job
			}
		}
		succeeded := 0
		for _, q := range ps {
			if q.EndBy == eventlog.Succeeded {
				succeeded++
			}
		}
		if succeeded >= p.Size {
			completed++
		} else if firstIncomplete == "" {
			firstIncomplete = job
		}
	}
	v := Verdict{
		Measurements: map[string]float64{
			"gangs":           float64(len(p.Jobs)),
			"gangs_completed": float64(completed),
			"max_partial_s":   maxPartial,
		},
		EvidenceSeq: evidence(groups...),
	}
	switch {
	case maxPartial > p.GraceSec && firstIncomplete != "":
		v.Outcome = NotSupported
		v.Reason = fmt.Sprintf("gang %s was partly placed for %s s, and gang %s did not complete", worst, fmtSec(maxPartial), firstIncomplete)
	case firstIncomplete != "":
		v.Outcome = NotSupported
		v.Reason = fmt.Sprintf("gang %s did not complete before the probe ended", firstIncomplete)
	case maxPartial > p.GraceSec:
		v.Outcome = Partial
		v.Reason = fmt.Sprintf("gang %s was partly placed for %s s; all %d gangs completed", worst, fmtSec(maxPartial), len(p.Jobs))
	default:
		v.Outcome = Supported
		v.Reason = fmt.Sprintf("no gang was partly placed for more than %s s, and all %d gangs completed", fmtSec(p.GraceSec), len(p.Jobs))
	}
	return v, nil
}
