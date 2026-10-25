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

package claims_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/claims"
	"github.com/NVIDIA/knavigator/pkg/verdict"
)

const valid = `scheduler: vanilla
version: Kubernetes v1.37.0
claims:
  gang:
    claim: Pods of a group are scheduled together or not at all.
    citation: https://example.org/gang
    expected: supported
    mechanism: Workload API gang
  priority:
    claim: Higher-priority pods are ordered ahead in the scheduling queue.
    citation: https://example.org/priority
    expected: supported
    mechanism: PriorityClass
  preemption:
    claim: The scheduler evicts lower-priority pods to make room.
    citation: https://example.org/preemption
    expected: partial
    mechanism: PriorityClass with PreemptLowerPriority
  fair-share:
    claim: No tenant fair-share mechanism is documented.
    citation: https://example.org/fair-share
    expected: not_supported
    mechanism: namespaces as tenants, no policy
  backfill:
    claim: No backfill or reservation mechanism is documented.
    citation: https://example.org/backfill
    expected: partial
    mechanism: plain Jobs
  topology:
    claim: Required pod affinity keeps pods in one topology domain.
    citation: https://example.org/topology
    expected: supported
    mechanism: required pod affinity on the rack label
`

func TestParse(t *testing.T) {
	f, err := claims.Parse([]byte(valid))
	require.NoError(t, err)
	require.Equal(t, "vanilla", f.Scheduler)
	require.Equal(t, "Kubernetes v1.37.0", f.Version)
	require.Len(t, f.Claims, 6)
	require.Equal(t, claims.Claim{
		Claim:     "No tenant fair-share mechanism is documented.",
		Citation:  "https://example.org/fair-share",
		Expected:  verdict.NotSupported,
		Mechanism: "namespaces as tenants, no policy",
	}, f.Claims["fair-share"])
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		err  string
	}{
		{
			name: "a capability knavigator does not define",
			in:   strings.Replace(valid, "  backfill:", "  removed:", 1),
			err:  `claims: unknown capability "removed"`,
		},
		{
			name: "a file that leaves a capability out",
			in:   valid[:strings.Index(valid, "  topology:")],
			err:  `claims: missing capability "topology"`,
		},
		{
			name: "a citation that is not https",
			in:   strings.Replace(valid, "https://example.org/priority", "http://example.org/priority", 1),
			err:  `claims: capability "priority": citation "http://example.org/priority" is not an https URL`,
		},
		{
			name: "an expected outcome outside the three",
			in:   strings.Replace(valid, "expected: partial\n    mechanism: plain Jobs", "expected: maybe\n    mechanism: plain Jobs", 1),
			err:  `claims: capability "backfill": expected "maybe", want supported, partial or not_supported`,
		},
		{
			name: "an empty claim",
			in:   strings.Replace(valid, "claim: Pods of a group are scheduled together or not at all.", `claim: ""`, 1),
			err:  `claims: capability "gang": claim is empty`,
		},
		{
			name: "an empty mechanism",
			in:   strings.Replace(valid, "mechanism: plain Jobs", `mechanism: ""`, 1),
			err:  `claims: capability "backfill": mechanism is empty`,
		},
		{
			name: "a file without a version",
			in:   strings.Replace(valid, "version: Kubernetes v1.37.0", `version: ""`, 1),
			err:  "claims: scheduler and version must both be set",
		},
		{
			name: "a file without a scheduler",
			in:   strings.Replace(valid, "scheduler: vanilla", `scheduler: ""`, 1),
			err:  "claims: scheduler and version must both be set",
		},
		{
			name: "a field the format does not define",
			in:   strings.Replace(valid, "version: Kubernetes v1.37.0", "version: Kubernetes v1.37.0\nvendor: someone", 1),
			err:  `claims: error unmarshaling JSON: while decoding JSON: json: unknown field "vendor"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := claims.Parse([]byte(tc.in))
			require.EqualError(t, err, tc.err)
		})
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vanilla.yaml")
	require.NoError(t, os.WriteFile(path, []byte(valid), 0o600))
	f, err := claims.Load(path)
	require.NoError(t, err)
	require.Equal(t, "vanilla", f.Scheduler)

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte(valid[:strings.Index(valid, "  topology:")]), 0o600))
	_, err = claims.Load(bad)
	require.EqualError(t, err, bad+`: claims: missing capability "topology"`)

	missing := filepath.Join(t.TempDir(), "missing.yaml")
	_, err = claims.Load(missing)
	require.EqualError(t, err, "open "+missing+": no such file or directory")
}

// shipped is what a shipped claims file must say for one capability.
type shipped struct {
	expected verdict.Outcome
	citation string
}

// TestShippedClaims loads every file under resources/claims and pins its
// scheduler, version, and each capability's expected outcome and citation,
// as recorded by the Scheduling Capabilities Suite at e5968b1.
func TestShippedClaims(t *testing.T) {
	tests := []struct {
		file, scheduler, version string
		claims                   map[string]shipped
		// quotes are substrings each named claim must contain
		quotes map[string]string
	}{
		{
			file: "vanilla.yaml", scheduler: "vanilla",
			version: "Kubernetes v1.37.0 (kindest/node v1.37.0)",
			claims: map[string]shipped{
				"gang":       {verdict.Supported, "https://kubernetes.io/docs/concepts/scheduling-eviction/gang-scheduling/"},
				"priority":   {verdict.Supported, "https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/"},
				"preemption": {verdict.Supported, "https://kubernetes.io/docs/concepts/workloads/workload-api/disruption-and-priority/"},
				"fair-share": {verdict.NotSupported, "https://kubernetes.io/docs/concepts/policy/resource-quotas/"},
				"backfill":   {verdict.Partial, "https://kubernetes.io/docs/concepts/scheduling-eviction/gang-scheduling/"},
				"topology":   {verdict.Supported, "https://kubernetes.io/docs/concepts/scheduling-eviction/topology-aware-scheduling/"},
			},
			quotes: map[string]string{"backfill": "allowing other workloads to be scheduled in the meantime"},
		},
		{
			file: "kueue.yaml", scheduler: "kueue",
			version: "Kueue v0.20.1 on Kubernetes v1.37.0",
			claims: map[string]shipped{
				"gang":       {verdict.Supported, "https://kueue.sigs.k8s.io/v0.20/docs/concepts/all_or_nothing/"},
				"priority":   {verdict.Supported, "https://kueue.sigs.k8s.io/v0.20/docs/concepts/cluster_queue/"},
				"preemption": {verdict.Supported, "https://kueue.sigs.k8s.io/v0.20/docs/concepts/preemption/"},
				"fair-share": {verdict.Supported, "https://kueue.sigs.k8s.io/v0.20/docs/concepts/cluster_queue/"},
				"backfill":   {verdict.Partial, "https://kueue.sigs.k8s.io/v0.20/docs/concepts/cluster_queue/"},
				"topology":   {verdict.Supported, "https://kueue.sigs.k8s.io/v0.20/docs/concepts/topology_aware_scheduling/"},
			},
			quotes: map[string]string{
				"fair-share": "reclaimWithinCohort determines whether a pending Workload can preempt",
				"priority":   "creationTimestamp. [...] BestEffortFIFO",
			},
		},
		{
			file: "kai.yaml", scheduler: "kai",
			version: "KAI Scheduler v0.18.5 on Kubernetes v1.37.0",
			claims: map[string]shipped{
				"gang":       {verdict.Supported, "https://github.com/kai-scheduler/KAI-Scheduler/blob/v0.18.5/docs/developer/pod-grouper.md#L18"},
				"priority":   {verdict.Supported, "https://github.com/kai-scheduler/KAI-Scheduler/blob/v0.18.5/docs/priority/README.md#L35"},
				"preemption": {verdict.Supported, "https://github.com/kai-scheduler/KAI-Scheduler/blob/v0.18.5/docs/scheduling-deep-dive/README.md#L56"},
				"fair-share": {verdict.Supported, "https://github.com/kai-scheduler/KAI-Scheduler/blob/v0.18.5/docs/fairness/README.md#L69"},
				"backfill":   {verdict.Partial, "https://github.com/kai-scheduler/KAI-Scheduler/blob/v0.18.5/docs/developer/designs/queue-admission-gating/README.md#L84"},
				"topology":   {verdict.Supported, "https://github.com/kai-scheduler/KAI-Scheduler/blob/v0.18.5/pkg/apis/scheduling/v2alpha2/podgroup_types.go#L426-L429"},
			},
			quotes: map[string]string{
				"fair-share": "Workloads from queues under their quota can evict workloads from queues that have exceeded their quota.",
			},
		},
		{
			file: "slurm-bridge.yaml", scheduler: "slurm-bridge",
			version: "slurm-bridge v1.2.3 and slurm-operator v1.2.3 with Slurm 26.05.4 on Kubernetes v1.37.0",
			claims: map[string]shipped{
				"gang":       {verdict.Supported, "https://github.com/SlinkyProject/slurm-bridge/blob/v1.2.3/docs/workload.md#L183"},
				"priority":   {verdict.Supported, "https://github.com/SchedMD/slurm/blob/slurm-26-05-4-1/doc/html/sched_config.shtml#L16"},
				"preemption": {verdict.Supported, "https://github.com/SchedMD/slurm/blob/slurm-26-05-4-1/doc/html/preempt.shtml#L179"},
				"fair-share": {verdict.NotSupported, "https://github.com/SchedMD/slurm/blob/slurm-26-05-4-1/doc/html/priority_multifactor.shtml#L253"},
				"backfill":   {verdict.NotSupported, "https://github.com/SchedMD/slurm/blob/slurm-26-05-4-1/doc/man/man5/slurm.conf.5#L4976"},
				"topology":   {verdict.Supported, "https://github.com/SchedMD/slurm/blob/slurm-26-05-4-1/doc/html/topology.shtml#L38"},
			},
			quotes: map[string]string{
				"fair-share": "Computing the fair-share factor requires the installation and operation of the Slurm Accounting Database",
			},
		},
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "resources", "claims", "*.yaml"))
	require.NoError(t, err)
	require.Len(t, files, len(tests), "resources/claims holds a file this test does not pin")
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			f, err := claims.Load(filepath.Join("..", "..", "resources", "claims", tc.file))
			require.NoError(t, err)
			require.Equal(t, tc.scheduler, f.Scheduler)
			require.Equal(t, tc.version, f.Version)
			require.Len(t, f.Claims, len(tc.claims))
			for c, want := range tc.claims {
				require.Equal(t, want.expected, f.Claims[c].Expected, c)
				require.Equal(t, want.citation, f.Claims[c].Citation, c)
			}
			for c, q := range tc.quotes {
				require.Contains(t, f.Claims[c].Claim, q, c)
			}
		})
	}
}
