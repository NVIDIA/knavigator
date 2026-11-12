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

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/engine"
	"github.com/NVIDIA/knavigator/pkg/results"
)

func TestSubmittedJobs(t *testing.T) {
	eng, err := engine.New(nil, nil, true)
	require.NoError(t, err)
	info := engine.NewObjInfo([]string{"job1"}, "default", nil, 2, "job1-.*")
	info.Class, info.Trace = "training", "b5a2887c4f7c"
	require.NoError(t, eng.SetObjInfo("trace-1", info))

	require.Equal(t, []results.Job{{Name: "default/job1", Namespace: "default", Pods: 2, PodRegexp: "job1-.*",
		Class: "training", Trace: "b5a2887c4f7c"}}, submittedJobs(eng))
}

func TestSpeedUp(t *testing.T) {
	w := &config.Workflow{Tasks: []*config.Task{
		{Type: "Sleep", Params: map[string]interface{}{"timeout": "2m"}},
		{Type: "SubmitObj", Params: map[string]interface{}{"params": map[string]interface{}{"ttl": "5m", "workers": 4}}},
		{Type: "CheckPod", Params: map[string]interface{}{"timeout": "10m"}},
		{Type: "SubmitTrace", Params: map[string]interface{}{"refTaskId": "job"}},
	}}

	require.NoError(t, speedUp([]*config.Workflow{w}, 60))

	require.Equal(t, "2s", w.Tasks[0].Params["timeout"])
	require.Equal(t, "5s", w.Tasks[1].Params["params"].(map[string]interface{})["ttl"])
	require.Equal(t, 4, w.Tasks[1].Params["params"].(map[string]interface{})["workers"])
	require.Equal(t, "10m", w.Tasks[2].Params["timeout"])
	require.Equal(t, 60.0, w.Tasks[3].Params["speedup"])
}

func TestSpeedUpTraceMismatch(t *testing.T) {
	trace := func(v interface{}) []*config.Workflow {
		return []*config.Workflow{{Tasks: []*config.Task{
			{Type: "SubmitTrace", ID: "trace", Params: map[string]interface{}{"speedup": v}},
		}}}
	}
	require.NoError(t, speedUp(trace(60), 60))
	require.Error(t, speedUp(trace(1), 60))
	require.Error(t, speedUp(trace(60), 1))
}

func TestValidateRackLabel(t *testing.T) {
	args := func(label, results string) *Args {
		return &Args{workflow: "w.yml", speedup: 1, results: results, rackLabel: label}
	}
	require.NoError(t, validate(args("example.com/rack", "out")))
	require.EqualError(t, validate(args("example.com/rack", "")), "'results.rack-label' requires 'results'")
	require.ErrorContains(t, validate(args("example.com/rack=rack-1", "out")), "'results.rack-label' must be a label key")
	require.ErrorContains(t, validate(args("Bad Key", "out")),
		"'results.rack-label' must be a label key such as example.com/rack: name part must consist of alphanumeric characters")
}

func TestValidateBaseline(t *testing.T) {
	require.EqualError(t, validate(&Args{baseline: "out/volcano-*", workflow: "w.yml", speedup: 1}),
		"'aggregate.baseline' requires 'aggregate'")
}

func TestAggregateBaseline(t *testing.T) {
	root := t.TempDir()
	for name, makespan := range map[string]string{"a-1": "100", "a-2": "102", "b-1": "120", "b-2": "124"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name, "results.json"), []byte(`{"makespan_sec": `+makespan+`}`), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "a-run.log"), nil, 0o644))
	a, b := filepath.Join(root, "a-*"), filepath.Join(root, "b-*")

	var out bytes.Buffer
	require.NoError(t, aggregate(&out, a, b))
	require.Contains(t, out.String(), "{\n  \"makespan_sec\": {")
	require.Contains(t, out.String(), `"vs_baseline": {
      "n": 2,
      "diff": -21,
      "ci95": [
        -24,
        -18
      ],
      "clear": false
    }`)
	var got map[string]results.Spread
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Equal(t, 2, got["makespan_sec"].N, "the log file is not a run")
	require.Equal(t, -21.0, got["makespan_sec"].VsBaseline.Diff)

	out.Reset()
	require.NoError(t, aggregate(&out, a, ""))
	require.NotContains(t, out.String(), "vs_baseline")
	var plain map[string]results.Spread
	require.NoError(t, json.Unmarshal(out.Bytes(), &plain))
	require.Equal(t, 2, plain["makespan_sec"].N)
	require.Nil(t, plain["makespan_sec"].VsBaseline)

	out.Reset()
	require.NoError(t, aggregate(&out, a, filepath.Join(root, "b-1")))
	require.Contains(t, out.String(), `"diff": -19,
      "ci95": null,`, "one baseline run has no interval")

	require.EqualError(t, aggregate(&out, filepath.Join(root, "c-*"), b), "no directories match \""+filepath.Join(root, "c-*")+"\"")
	require.EqualError(t, aggregate(&out, a, "["), `"[": syntax error in pattern`)
	require.EqualError(t, aggregate(&out, filepath.Join(root, "*"), b), filepath.Join(root, "b-1")+" is both a run and a baseline run")
	require.NoError(t, os.Remove(filepath.Join(root, "b-2", "results.json")))
	require.ErrorIs(t, aggregate(&out, a, b), os.ErrNotExist)
}
