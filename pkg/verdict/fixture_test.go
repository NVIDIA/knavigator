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

package verdict_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/eventlog"
	"github.com/NVIDIA/knavigator/pkg/verdict"
)

// t0 is the fixtures' time origin; at(s) is t0 plus s seconds.
var t0 = time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)

func at(s float64) time.Time {
	return t0.Add(time.Duration(math.Round(s * float64(time.Second))))
}

// pod describes one fixture pod: its job, tenant and GPUs, and the second
// (after t0) of each transition; a negative second means "never".
type pod struct {
	name, job, tenant                string
	gpus                             int64
	node                             string
	created, scheduled, running, end float64
	endBy                            eventlog.Transition
	later                            []step
}

// step is one more transition of a pod, at a second after t0: a second
// terminal transition after end, as a recorder writes for a pod that
// completes and is then deleted.
type step struct {
	at float64
	tr eventlog.Transition
}

// job describes one fixture Job object: its tenant, total GPUs, and the
// second it was created and ended (negative: never ended).
type job struct {
	name, tenant string
	gpus         int64
	created, end float64
}

// fixture turns pods into a probe's events, ordered by time, with seq
// counting from 1.
func fixture(probe string, pods ...pod) []eventlog.Event {
	return fixtureJobs(probe, nil, pods...)
}

// fixtureJobs is fixture with Job events as well, as a recorder writes
// them for every job (under Kueue, a queued job has a Job and no pods).
func fixtureJobs(probe string, jobs []job, pods ...pod) []eventlog.Event {
	type item struct {
		at float64
		p  pod
		j  *job
		tr eventlog.Transition
	}
	var items []item
	for i := range jobs {
		j := &jobs[i]
		items = append(items, item{at: j.created, j: j, tr: eventlog.Created})
		if j.end >= 0 {
			items = append(items, item{at: j.end, j: j, tr: eventlog.Succeeded})
		}
	}
	for _, p := range pods {
		items = append(items, item{at: p.created, p: p, tr: eventlog.Created})
		if p.scheduled >= 0 {
			items = append(items, item{at: p.scheduled, p: p, tr: eventlog.Scheduled})
		}
		if p.running >= 0 {
			items = append(items, item{at: p.running, p: p, tr: eventlog.Running})
		}
		if p.end >= 0 {
			items = append(items, item{at: p.end, p: p, tr: p.endBy})
		}
		for _, s := range p.later {
			items = append(items, item{at: s.at, p: p, tr: s.tr})
		}
	}
	// stable insertion sort by time keeps same-time items in input order
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].at < items[j-1].at; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	out := make([]eventlog.Event, 0, len(items))
	for i, it := range items {
		if it.j != nil {
			out = append(out, eventlog.Event{
				Schema: 1, Seq: int64(i + 1), RunID: "fixture", ObservedAt: at(it.at),
				Kind: "Job", Namespace: probe, Name: it.j.name, UID: "uid-job-" + it.j.name,
				Probe: probe, Job: it.j.name, Tenant: it.j.tenant, GPUs: it.j.gpus, Transition: it.tr,
			})
			continue
		}
		e := eventlog.Event{
			Schema: 1, Seq: int64(i + 1), RunID: "fixture", ObservedAt: at(it.at),
			Kind: "Pod", Namespace: probe, Name: it.p.name, UID: "uid-" + it.p.name,
			Probe: probe, Job: it.p.job, Tenant: it.p.tenant, GPUs: it.p.gpus, Transition: it.tr,
		}
		if it.tr == eventlog.Scheduled {
			e.Node = it.p.node
		}
		out = append(out, e)
	}
	return out
}

// mustBuild is verdict.Build for fixtures that hold one identity per job
// name; it panics on an error, so a broken fixture fails its test.
func mustBuild(evs []eventlog.Event, probe string, until time.Time) verdict.Timeline {
	tl, err := verdict.Build(evs, probe, until)
	if err != nil {
		panic(err)
	}
	return tl
}

// done is a pod that ran to completion on node.
func done(name, job string, node string, created, scheduled, running, end float64) pod {
	return pod{name: name, job: job, tenant: "t1", gpus: 8, node: node, created: created,
		scheduled: scheduled, running: running, end: end, endBy: eventlog.Succeeded}
}

// pending is a pod that was created and never scheduled.
func pending(name, job string, created float64) pod {
	return pod{name: name, job: job, tenant: "t1", gpus: 8, created: created,
		scheduled: -1, running: -1, end: -1}
}

// want is what a test case expects of a verdict. Empty fields are not
// checked.
type want struct {
	outcome verdict.Outcome
	// reason is the exact reason, checked when not empty.
	reason string
	// measurements must be the verdict's whole measurement map, when set.
	measurements map[string]float64
	// some holds measurements that must be equal; near those that must be
	// within 1e-9.
	some, near map[string]float64
	// evidence must be the verdict's whole evidence, when set; hasSeq lists
	// sequence numbers the evidence must contain.
	evidence []int64
	hasSeq   []int64
}

func check(t *testing.T, v verdict.Verdict, w want) {
	t.Helper()
	if w.outcome != "" {
		require.Equal(t, w.outcome, v.Outcome, v.Reason)
	}
	if w.reason != "" {
		require.Equal(t, w.reason, v.Reason)
	}
	if w.measurements != nil {
		require.Equal(t, w.measurements, v.Measurements)
	}
	for k, x := range w.some {
		require.Contains(t, v.Measurements, k)
		require.Equal(t, x, v.Measurements[k], k)
	}
	for k, x := range w.near {
		require.Contains(t, v.Measurements, k)
		require.InDelta(t, x, v.Measurements[k], 1e-9, k)
	}
	if w.evidence != nil {
		require.Equal(t, w.evidence, v.EvidenceSeq)
	}
	for _, s := range w.hasSeq {
		require.Contains(t, v.EvidenceSeq, s)
	}
}
