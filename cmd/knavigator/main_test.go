/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
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
	"math"
	"os"
	"path/filepath"
	"strings"
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

func TestValidateProfile(t *testing.T) {
	args := func(profile, results string) *Args {
		return &Args{workflow: "w.yml", speedup: 1, results: results, profile: profile}
	}
	require.NoError(t, validate(args("", "")))
	require.NoError(t, validate(args("kueue", "out")))
	require.NoError(t, validate(args("slurm-bridge", "out")))
	require.EqualError(t, validate(args("kueue", "")), "'results.profile' requires 'results'")
	for _, bad := range []string{"Kueue", "kueue v0.20", strings.Repeat("a", 64)} {
		require.ErrorContains(t, validate(args(bad, "out")), "'results.profile' must be a lowercase DNS label", bad)
	}
}

func TestValidateTopologyLabels(t *testing.T) {
	args := func(keys, results string) *Args {
		return &Args{workflow: "w.yml", speedup: 1, results: results, topologyKeys: keys}
	}
	require.NoError(t, validate(args("example.com/block, example.com/zone,rack", "out")))
	require.EqualError(t, validate(args("example.com/block", "")), "'results.topology-labels' requires 'results'")
	require.ErrorContains(t, validate(args("example.com/block,example.com/a=b,-bad", "out")),
		`'results.topology-labels' must list label keys such as example.com/block: "example.com/a=b"`)
}

func TestValidateImageNamespaces(t *testing.T) {
	args := func(namespaces, results string) *Args {
		return &Args{workflow: "w.yml", speedup: 1, results: results, imageNS: namespaces}
	}
	require.NoError(t, validate(args("kueue-system, kai-scheduler", "out")))
	require.EqualError(t, validate(args("kueue-system", "")), "'results.image-namespaces' requires 'results'")
	require.ErrorContains(t, validate(args("kueue-system,Kueue", "out")),
		`'results.image-namespaces' must list namespace names: "Kueue"`)
}

// The manifest lists the workflows in run order, a repeated one again, each with the digest of
// every template its RegisterObj tasks read, so a changed template changes the manifest.
func TestWorkflowsOf(t *testing.T) {
	dir := t.TempDir()
	tpl := filepath.Join(dir, "job.yaml")
	write := func(path, text string) { require.NoError(t, os.WriteFile(path, []byte(text), 0o644)) }
	write(tpl, "kind: Job\n")
	a, b := filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml")
	write(a, "name: a\ntasks:\n  - id: job\n    type: RegisterObj\n    params:\n      template: "+tpl+
		"\n  - id: missing\n    type: RegisterObj\n    params:\n      template: "+filepath.Join(dir, "missing.yaml")+
		"\n  - id: wait\n    type: Sleep\n    params:\n      template: "+tpl+"\n")
	write(b, "name: b\ntasks:\n  - id: wait\n    type: Sleep\n")
	load := func(paths string) []results.Workflow {
		workflows, err := config.NewFromPaths(paths, nil)
		require.NoError(t, err)
		return workflowsOf(workflows)
	}

	got := load(a + "," + b + "," + a)
	require.Len(t, got, 3)
	require.Equal(t, []string{a, b, a}, []string{got[0].Path, got[1].Path, got[2].Path})
	require.Equal(t, map[string]string{tpl: config.Digest([]byte("kind: Job\n"))}, got[0].Templates,
		"only RegisterObj templates, and not one that cannot be read")
	require.Equal(t, map[string]string{}, got[1].Templates)
	require.NotEqual(t, load(a+","+b), load(b+","+a))

	write(tpl, "kind: JobSet\n")
	require.NotEqual(t, got[0].Templates, load(a)[0].Templates)
	require.Equal(t, got[0].Digest, load(a)[0].Digest, "the workflow text did not change")
	require.Empty(t, workflowsOf(nil))
}

func TestValidateSpeedup(t *testing.T) {
	require.NoError(t, validate(&Args{workflow: "w.yml", speedup: 1}))
	for _, bad := range []float64{0.5, math.NaN(), math.Inf(1)} {
		require.EqualError(t, validate(&Args{workflow: "w.yml", speedup: bad}), "'speedup' must be a finite number, at least 1")
	}
}
