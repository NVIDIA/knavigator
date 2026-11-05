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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/eventlog"
	"github.com/NVIDIA/knavigator/pkg/verdict"
)

// gangRun is two completed gangs and a late pod of gang-b, observed at 58 s.
func gangRun() []eventlog.Event {
	return fixture("gang",
		done("a0", "gang-a", "n1", 0, 1.0, 2, 22), done("a1", "gang-a", "n2", 0, 1.5, 2, 22),
		done("b0", "gang-b", "n1", 0, 22.5, 23.5, 44), done("b1", "gang-b", "n2", 0, 23.0, 24, 44),
		done("late", "gang-b", "n3", 55, 56, 57, 58),
	)
}

func TestEvaluateDecodesParamsAndAppliesTheWindow(t *testing.T) {
	v, err := verdict.Evaluate(gangRun(), verdict.Probe{
		Capability: "gang",
		Params:     json.RawMessage(`{"jobs":["gang-a","gang-b"],"size":2,"grace_sec":5}`),
		WindowEnd:  at(50),
	})
	require.NoError(t, err)
	require.Equal(t, "gang", v.Capability)
	require.Equal(t, verdict.Supported, v.Outcome)
	// the late pod's four events fall after the window
	require.Len(t, v.EvidenceSeq, 16)
}

func TestEvaluateJudgesTheTopologyControlUnderItsOwnName(t *testing.T) {
	evs := fixture("topology-control",
		attempts(attempt("t1", "n3", "n4", 0), attempt("t2", "n2", "n3", 30), attempt("t3", "n3", "n4", 60))...)
	v, err := verdict.Evaluate(evs, verdict.Probe{
		Capability: "topology-control",
		Params: json.RawMessage(`{"attempt_jobs":["t1","t2","t3"],"pods_per_job":2,` +
			`"rack_of":{"n1":"rack-a","n2":"rack-a","n3":"rack-b","n4":"rack-b"}}`),
		WindowEnd: at(200),
	})
	require.NoError(t, err)
	require.Equal(t, "topology-control", v.Capability)
	require.Equal(t, verdict.Partial, v.Outcome)
	require.Equal(t, "2 of 3 attempts placed every pod in one rack", v.Reason)
}

func TestEvaluateJudgesTheGangControlUnderItsOwnName(t *testing.T) {
	evs := fixture("gang-control",
		done("a0", "gang-a", "n1", 0, 1.0, 2, 22), done("a1", "gang-a", "n2", 0, 1.5, 2, 22),
		done("b0", "gang-b", "n1", 0, 22.5, 23.5, 44), done("b1", "gang-b", "n2", 0, 23.0, 24, 44),
	)
	v, err := verdict.Evaluate(evs, verdict.Probe{
		Capability: "gang-control",
		Params:     json.RawMessage(`{"jobs":["gang-a","gang-b"],"size":2,"grace_sec":5}`),
		WindowEnd:  at(60),
	})
	require.NoError(t, err)
	require.Equal(t, "gang-control", v.Capability)
	require.Equal(t, verdict.Supported, v.Outcome)
	require.Equal(t, "no gang was partly placed for more than 5 s, and all 2 gangs completed", v.Reason)
}

func TestEvaluateErrors(t *testing.T) {
	tests := []struct {
		name  string
		probe verdict.Probe
		err   string
	}{
		{
			name:  "a parameter the probe does not define",
			probe: verdict.Probe{Capability: "gang", Params: json.RawMessage(`{"jobs":["gang-a"],"size":2,"grace_sec":5,"colour":"red"}`), WindowEnd: at(50)},
			err:   `gang: params: json: unknown field "colour"`,
		},
		{
			name:  "an unknown capability",
			probe: verdict.Probe{Capability: "teleport", Params: json.RawMessage(`{}`), WindowEnd: at(50)},
			err:   "teleport: unknown capability",
		},
		{
			name:  "a probe without a window end",
			probe: verdict.Probe{Capability: "gang", Params: json.RawMessage(`{}`)},
			err:   "gang: window_end is missing",
		},
		{
			name:  "gang size below 2",
			probe: verdict.Probe{Capability: "gang", Params: json.RawMessage(`{"jobs":["gang-a","gang-b"],"size":0,"grace_sec":5}`), WindowEnd: at(50)},
			err:   "gang: size 0, want 2 or more",
		},
		{
			name:  "priority without a high job",
			probe: verdict.Probe{Capability: "priority", Params: json.RawMessage(`{"high_job":"","queued_jobs":["q1"]}`), WindowEnd: at(50)},
			err:   "priority: high_job is empty",
		},
		{
			name:  "preemption with no high pods",
			probe: verdict.Probe{Capability: "preemption", Params: json.RawMessage(`{"high_job":"high","high_pods":0,"victim_job":"victim","grace_sec":5}`), WindowEnd: at(50)},
			err:   "preemption: high_pods 0, want 1 or more",
		},
		{
			name: "fair-share with a zero window",
			probe: verdict.Probe{Capability: "fair-share", Params: json.RawMessage(
				`{"tenant_b":"b","entitlement_gpus":16,"job_length_sec":60,"tolerance_sec":5,"capacity_gpus":32,"window_sec":0}`), WindowEnd: at(50)},
			err: "fair-share: job_length_sec and window_sec must be positive, tolerance_sec not negative",
		},
		{
			name: "backfill with a negative tolerance",
			probe: verdict.Probe{Capability: "backfill", Params: json.RawMessage(
				`{"big_job":"big","big_pods":4,"blocker_jobs":["blk0"],"small_jobs":["s0"],"tolerance_sec":-1}`), WindowEnd: at(50)},
			err: "backfill: tolerance_sec -1 is negative",
		},
		{
			name:  "topology with no pods per job",
			probe: verdict.Probe{Capability: "topology", Params: json.RawMessage(`{"attempt_jobs":["t1"],"pods_per_job":0,"rack_of":{"n1":"rack-a"}}`), WindowEnd: at(50)},
			err:   "topology: pods_per_job 0, want 1 or more",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verdict.Evaluate(gangRun(), tc.probe)
			require.EqualError(t, err, tc.err)
		})
	}
}
