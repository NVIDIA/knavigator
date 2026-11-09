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

// Package verdict turns a run's event log into observed verdicts: for each
// capability probe, whether the scheduler supported the capability, partly
// supported it or did not, with the measurements and the events the verdict
// rests on. Every function here is pure: the same events and parameters
// give the same verdict, so a run's verdicts can be rebuilt from its event
// log later or on another machine.
//
// Within a probe a job is known by its name alone: probe parameters list
// jobs by name, and pods are matched to their job by its name. A probe's
// job names must therefore be unique; Build rejects a job name recorded in
// two namespaces or for two tenants rather than merge two jobs into one.
package verdict

import (
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/NVIDIA/knavigator/pkg/eventlog"
)

// Outcome is the observed verdict for one capability.
type Outcome string

// The three outcomes a probe can record.
const (
	Supported    Outcome = "supported"
	Partial      Outcome = "partial"
	NotSupported Outcome = "not_supported"
)

// Valid reports whether o is one of the three outcomes.
func (o Outcome) Valid() bool {
	return o == Supported || o == Partial || o == NotSupported
}

// Verdict is what one probe observed, with the numbers and the event
// sequence numbers it rests on.
type Verdict struct {
	Capability   string             `json:"capability"`
	Outcome      Outcome            `json:"outcome"`
	Reason       string             `json:"reason"`
	Measurements map[string]float64 `json:"measurements"`
	EvidenceSeq  []int64            `json:"evidence_seq"`
}

// Pod is one pod's life as the recorder saw it. A zero time means the
// recorder never saw that transition within the evidence window.
type Pod struct {
	UID       string
	Name      string
	Job       string
	Tenant    string
	Priority  int32
	GPUs      int64
	Node      string
	Created   time.Time
	Scheduled time.Time
	Running   time.Time
	End       time.Time
	EndBy     eventlog.Transition
	Seqs      []int64
}

var terminal = map[eventlog.Transition]bool{
	eventlog.Succeeded: true, eventlog.Failed: true, eventlog.Deleted: true,
	eventlog.Preempted: true, eventlog.Evicted: true,
}

// Pods folds the Pod events of one probe, observed no later than until,
// into one Pod per UID, in order of first appearance. The first time a
// transition is seen wins, and the first terminal transition sets End.
func Pods(evs []eventlog.Event, probe string, until time.Time) []*Pod {
	byUID := map[string]*Pod{}
	var order []*Pod
	for _, e := range evs {
		if e.Kind != "Pod" || e.Probe != probe || e.ObservedAt.After(until) {
			continue
		}
		p, ok := byUID[e.UID]
		if !ok {
			p = &Pod{UID: e.UID, Name: e.Name, Job: e.Job, Tenant: e.Tenant, Priority: e.Priority, GPUs: e.GPUs}
			byUID[e.UID] = p
			order = append(order, p)
		}
		p.Seqs = append(p.Seqs, e.Seq)
		switch {
		case e.Transition == eventlog.Created:
			setOnce(&p.Created, e.ObservedAt)
		case e.Transition == eventlog.Scheduled:
			if p.Scheduled.IsZero() {
				p.Scheduled = e.ObservedAt
				p.Node = e.Node
			}
		case e.Transition == eventlog.Running:
			setOnce(&p.Running, e.ObservedAt)
		case terminal[e.Transition]:
			if p.End.IsZero() {
				p.End = e.ObservedAt
				p.EndBy = e.Transition
			}
		}
	}
	return order
}

func setOnce(t *time.Time, v time.Time) {
	if t.IsZero() {
		*t = v
	}
}

// ofJob returns the pods of job, in order of first appearance.
func ofJob(pods []*Pod, job string) []*Pod {
	var out []*Pod
	for _, p := range pods {
		if p.Job == job {
			out = append(out, p)
		}
	}
	return out
}

// ofJobs returns the pods of every job in jobs, and the first job that has
// no recorded pod.
func ofJobs(pods []*Pod, jobs []string) ([]*Pod, string) {
	var out []*Pod
	for _, j := range jobs {
		ps := ofJob(pods, j)
		if len(ps) == 0 {
			return nil, j
		}
		out = append(out, ps...)
	}
	return out, ""
}

// nthScheduled returns the n-th earliest scheduling time among pods (n
// counts from 1), or false when fewer than n pods were scheduled.
func nthScheduled(pods []*Pod, n int) (time.Time, bool) {
	var ts []time.Time
	for _, p := range pods {
		if !p.Scheduled.IsZero() {
			ts = append(ts, p.Scheduled)
		}
	}
	if n < 1 || len(ts) < n {
		return time.Time{}, false
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	return ts[n-1], true
}

// evidence returns the sorted, de-duplicated event sequence numbers of pods.
func evidence(groups ...[]*Pod) []int64 {
	seen := map[int64]bool{}
	out := []int64{}
	for _, g := range groups {
		for _, p := range g {
			for _, s := range p.Seqs {
				if !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// seconds converts s seconds to a Duration, rounded to the nanosecond.
func seconds(s float64) time.Duration {
	return time.Duration(math.Round(s * float64(time.Second)))
}

// fmtSec renders seconds rounded to the millisecond, without trailing zeros.
func fmtSec(s float64) string {
	return strconv.FormatFloat(math.Round(s*1000)/1000, 'f', -1, 64)
}
