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
	"fmt"
	"time"

	"github.com/NVIDIA/knavigator/pkg/eventlog"
)

// Job is one job's life as recorded from its batch/v1 Job object. Under
// Kueue a queued job has no pods (Kueue suspends it), so a job's submission
// is its Job's creation, not its first pod's.
type Job struct {
	Name    string
	Tenant  string
	GPUs    int64
	Created time.Time
	End     time.Time
	Seqs    []int64
}

// Timeline is one probe's evidence up to the end of its window.
type Timeline struct {
	Pods []*Pod
	Jobs map[string]*Job
	End  time.Time
}

// Build folds a probe's events, observed no later than until, into its
// pods and jobs. A job seen only through its pods (no Job events, as in
// hand-built fixtures) gets a Job derived from them: created with its first
// pod, its pods' GPUs, and ended with its last pod when all of them ended.
// Build returns an error when one job name is recorded, by its Job or its
// pods, in two namespaces or for two tenants.
func Build(evs []eventlog.Event, probe string, until time.Time) (Timeline, error) {
	if err := oneIdentity(evs, probe, until); err != nil {
		return Timeline{}, err
	}
	t := Timeline{Pods: Pods(evs, probe, until), Jobs: map[string]*Job{}, End: until}
	for _, e := range evs {
		if e.Kind != "Job" || e.Probe != probe || e.ObservedAt.After(until) {
			continue
		}
		j, ok := t.Jobs[e.Name]
		if !ok {
			j = &Job{Name: e.Name, Tenant: e.Tenant, GPUs: e.GPUs}
			t.Jobs[e.Name] = j
		}
		j.Seqs = append(j.Seqs, e.Seq)
		switch e.Transition {
		case eventlog.Created:
			setOnce(&j.Created, e.ObservedAt)
		case eventlog.Succeeded, eventlog.Failed, eventlog.Deleted:
			setOnce(&j.End, e.ObservedAt)
		}
	}
	derived := map[string]*Job{}
	for _, p := range t.Pods {
		if _, recorded := t.Jobs[p.Job]; recorded {
			continue
		}
		j, ok := derived[p.Job]
		if !ok {
			j = &Job{Name: p.Job, Tenant: p.Tenant}
			derived[p.Job] = j
		}
		j.GPUs += p.GPUs
		if !p.Created.IsZero() && (j.Created.IsZero() || p.Created.Before(j.Created)) {
			j.Created = p.Created
		}
	}
	for name, j := range derived {
		allEnded := true
		for _, p := range ofJob(t.Pods, name) {
			if p.End.IsZero() {
				allEnded = false
			} else if p.End.After(j.End) {
				j.End = p.End
			}
		}
		if !allEnded {
			j.End = time.Time{}
		}
		t.Jobs[name] = j
	}
	return t, nil
}

// oneIdentity checks that the Job and Pod events of a probe, observed no
// later than until, record each job name in one namespace and for one
// tenant. A Job event names its job by the Job's name, a Pod event by its
// job label.
func oneIdentity(evs []eventlog.Event, probe string, until time.Time) error {
	type identity struct{ namespace, tenant string }
	first := map[string]identity{}
	for _, e := range evs {
		if (e.Kind != "Job" && e.Kind != "Pod") || e.Probe != probe || e.ObservedAt.After(until) {
			continue
		}
		name := e.Job
		if e.Kind == "Job" {
			name = e.Name
		}
		if name == "" {
			continue
		}
		id, seen := first[name]
		switch {
		case !seen:
			first[name] = identity{e.Namespace, e.Tenant}
		case id.namespace != e.Namespace:
			return fmt.Errorf("job %s is recorded in two namespaces, %q and %q", name, id.namespace, e.Namespace)
		case id.tenant != e.Tenant:
			return fmt.Errorf("job %s is recorded for two tenants, %q and %q", name, id.tenant, e.Tenant)
		}
	}
	return nil
}

// job returns a job's pods, and whether the job was seen at all (as a Job
// or through its pods). A job seen without pods was never admitted.
func (t Timeline) job(name string) ([]*Pod, bool) {
	ps := ofJob(t.Pods, name)
	_, seen := t.Jobs[name]
	return ps, seen || len(ps) > 0
}

// submitted is when a job was submitted: its Job's creation.
func (t Timeline) submitted(name string) time.Time {
	if j, ok := t.Jobs[name]; ok {
		return j.Created
	}
	return time.Time{}
}

// jobSeqs returns the Job events' sequence numbers of the named jobs, as
// pseudo-pods so evidence() can merge them with pod evidence.
func (t Timeline) jobSeqs(names ...string) []*Pod {
	var out []*Pod
	for _, n := range names {
		if j, ok := t.Jobs[n]; ok && len(j.Seqs) > 0 {
			out = append(out, &Pod{Seqs: j.Seqs})
		}
	}
	return out
}
