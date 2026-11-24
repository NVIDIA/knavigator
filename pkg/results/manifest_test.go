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

package results

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	log "k8s.io/klog/v2"
)

// suiteManifest and suiteProfileRun copy the Scheduling Capabilities Suite's manifest types
// (pkg/report/manifest.go at e5968b1). Decoding manifest.json into them fails, as the Suite's
// MergeManifest does, when a field the two share has another JSON type.
type suiteManifest struct {
	Schema    int                        `json:"schema"`
	RunID     string                     `json:"run_id"`
	GitCommit string                     `json:"git_commit"`
	GitDirty  bool                       `json:"git_dirty"`
	Host      map[string]string          `json:"host"`
	Tools     map[string]string          `json:"tools"`
	Fleet     json.RawMessage            `json:"fleet"`
	Profiles  map[string]suiteProfileRun `json:"profiles"`
}

type suiteProfileRun struct {
	Start             time.Time         `json:"start"`
	End               time.Time         `json:"end"`
	Cluster           string            `json:"cluster"`
	FeatureGates      map[string]bool   `json:"feature_gates"`
	RuntimeConfig     map[string]string `json:"runtime_config"`
	Images            map[string]string `json:"images"`
	Manifests         map[string]string `json:"manifests"`
	Scheduler         map[string]string `json:"scheduler"`
	ClockOffsetStartS float64           `json:"clock_offset_start_s"`
	ClockOffsetEndS   *float64          `json:"clock_offset_end_s,omitempty"`
	WatchErrors       int               `json:"watch_errors"`
}

// stubBuildInfo gives the binary's build info these VCS settings for the rest of the test; a test
// binary has none of its own.
func stubBuildInfo(t *testing.T, settings ...debug.BuildSetting) {
	t.Helper()
	old := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Settings: settings}, true }
	t.Cleanup(func() { readBuildInfo = old })
}

// fakeServerVersion makes the fake API server report gitVersion on /version.
func fakeServerVersion(client *fake.Clientset, gitVersion string) {
	client.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: gitVersion}
}

// kindNode is kind's control-plane node, a real node with no GPUs and kind's providerID.
func kindNode(name string) *corev1.Node {
	n := &corev1.Node{}
	n.Name = name
	n.Spec.ProviderID = "kind://docker/kind/" + name
	return n
}

// kwokNode is a KWOK GPU node as the virtual-nodes chart makes it, with the fake annotation and no
// providerID.
func kwokNode(name, gpus string, labels map[string]string) *corev1.Node {
	n := gpuNode(name, gpus)
	n.Annotations = map[string]string{kwokNodeAnnotation: "fake"}
	n.Labels = labels
	return n
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, v))
}

// kindKWOKCluster is a kind cluster with two KWOK GPU nodes in two racks and one block, and a
// control-plane pod that runs a scheduler image.
func kindKWOKCluster() *fake.Clientset {
	scheduler := containerPod("kube-system", "kube-scheduler-kind", "kube-scheduler", 0, controlPlane())
	scheduler.Status.ContainerStatuses[0].Image = "registry.k8s.io/kube-scheduler:v1.37.0"
	scheduler.Status.ContainerStatuses[0].ImageID = "sha256:5f1e"
	return fake.NewSimpleClientset(kindNode("kind-control-plane"), scheduler,
		kwokNode("n1", "8", map[string]string{"example.com/rack": "a", "example.com/block": "b1"}),
		kwokNode("n2", "8", map[string]string{"example.com/rack": "b", "example.com/block": "b1"}))
}

// Finish on a kind cluster with KWOK GPU nodes, under an API server clock 1000s behind the host's,
// writes a manifest.json that the Suite's types decode and whose fields each come from their source.
func TestFinishWritesManifest(t *testing.T) {
	fastPoll(t)
	stubBuildInfo(t, debug.BuildSetting{Key: "vcs.revision", Value: "7707ed4"}, debug.BuildSetting{Key: "vcs.modified", Value: "true"})
	client := kindKWOKCluster()
	fakeServerVersion(client, "v1.37.0")
	skewClock(client, -1000*time.Second)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	rec.SetRackLabel("example.com/rack")
	rec.SetRunInfo(RunInfo{
		Profile:        "kueue",
		TopologyLabels: []string{"example.com/block"},
		Workflows:      []Workflow{{Path: "run-test.yaml", Digest: "sha256:9d2c", Templates: map[string]string{"job.yaml": "sha256:4a1b"}}},
		Vars:           map[string]string{"nodes": "2"},
	})

	dir := filepath.Join(t.TempDir(), "kueue-1")
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

	var suite suiteManifest
	readJSON(t, filepath.Join(dir, "manifest.json"), &suite)
	require.Equal(t, 1, suite.Schema)
	require.Equal(t, "kueue-1", suite.RunID)
	require.Equal(t, "7707ed4", suite.GitCommit)
	require.True(t, suite.GitDirty)
	require.Equal(t, map[string]string{"kubernetes": "v1.37.0"}, suite.Tools)
	require.Len(t, suite.Profiles, 1)
	run := suite.Profiles["kueue"]
	require.Equal(t, "kind", run.Cluster, "the KWOK nodes have no providerID, so the kind node names it")
	require.Equal(t, map[string]string{"registry.k8s.io/kube-scheduler:v1.37.0": "sha256:5f1e"}, run.Images)
	require.Zero(t, run.WatchErrors)

	// The window is on the API server's clock, which runs 1000s behind the host's.
	api := time.Now().Add(-1000 * time.Second)
	require.WithinDuration(t, api, run.Start, 5*time.Second)
	require.WithinDuration(t, api, run.End, 5*time.Second)
	require.False(t, run.End.Before(run.Start))
	require.Equal(t, time.UTC, run.Start.Location())

	var own manifest
	readJSON(t, filepath.Join(dir, "manifest.json"), &own)
	require.Equal(t, fleet{Mode: fleetKWOK, Nodes: 2, GPUsPerNode: map[int]int{8: 2},
		Labels: map[string]int{"example.com/rack": 2, "example.com/block": 1}}, own.Fleet)
	require.Equal(t, workload{Workflows: []Workflow{{Path: "run-test.yaml", Digest: "sha256:9d2c", Templates: map[string]string{"job.yaml": "sha256:4a1b"}}},
		Vars: map[string]string{"nodes": "2"}, Speedup: 1, BSLDFloorSec: 600}, own.Workload)

	// results.json counts the same GPUs as the fleet does.
	require.Equal(t, 16.0, readSummary(t, dir).GPUCapacity)
}

// A run never described by SetRunInfo is filed under "unknown", its maps are {} and not null, and a
// binary without a VCS stamp writes neither git_commit nor git_dirty.
func TestFinishWritesManifestWithoutRunInfo(t *testing.T) {
	fastPoll(t)
	stubBuildInfo(t)
	client := fake.NewSimpleClientset()
	fakeServerVersion(client, "v1.37.0")
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

	var raw map[string]json.RawMessage
	readJSON(t, filepath.Join(dir, "manifest.json"), &raw)
	require.NotContains(t, raw, "git_commit")
	require.NotContains(t, raw, "git_dirty")
	require.JSONEq(t, `{"nodes": 0, "gpus_per_node": {}, "labels": {}}`, string(raw["fleet"]), "no GPU node leaves out the mode")
	var profiles map[string]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["profiles"], &profiles))
	require.Len(t, profiles, 1)
	require.Contains(t, profiles, UnknownProfile)
	require.JSONEq(t, `{}`, string(profiles[UnknownProfile]["images"]))
	require.NotContains(t, profiles[UnknownProfile], "cluster")
	require.JSONEq(t, `{"workflows": [], "vars": {}, "speedup": 1, "bsld_floor_sec": 600, "allow_leftovers": false}`, string(raw["workload"]))
}

// allow_leftovers follows -results.allow-leftovers even when no pod from before the run is left, and
// bsld_floor_sec is the floor that Finish was given.
func TestManifestRecordsResultsSettings(t *testing.T) {
	fastPoll(t)
	rec, err := Start(context.Background(), fake.NewSimpleClientset(), HarnessOptions{AllowLeftovers: true})
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, 30*time.Minute))

	var m manifest
	readJSON(t, filepath.Join(dir, "manifest.json"), &m)
	require.True(t, m.Workload.AllowLeftovers)
	require.Equal(t, 1800.0, m.Workload.BSLDFloorSec)
}

// speedup is written at 1 and at 60, while results.json writes it only above 1, and start and end
// stay real API server times at any speedup.
func TestManifestRecordsSpeedup(t *testing.T) {
	fastPoll(t)
	for _, speedup := range []float64{1, 60} {
		t.Run(fmt.Sprint(speedup), func(t *testing.T) {
			rec, err := Start(context.Background(), fake.NewSimpleClientset(), HarnessOptions{})
			require.NoError(t, err)
			dir := t.TempDir()
			require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, speedup, 0, DefaultBSLDFloor))

			var m manifest
			readJSON(t, filepath.Join(dir, "manifest.json"), &m)
			require.Equal(t, speedup, m.Workload.Speedup)
			require.Equal(t, DefaultBSLDFloor.Seconds(), m.Workload.BSLDFloorSec, "the floor stays in simulated time")
			run := m.Profiles[UnknownProfile]
			require.WithinDuration(t, time.Now(), run.Start, 5*time.Second)
			require.WithinDuration(t, time.Now(), run.End, 5*time.Second)

			var results map[string]json.RawMessage
			readJSON(t, filepath.Join(dir, "results.json"), &results)
			if speedup > 1 {
				require.Contains(t, results, "speedup")
			} else {
				require.NotContains(t, results, "speedup")
			}
		})
	}
}

// clock_offset_*_s equal results.json's harness readings, and both are absent when the clock
// cannot be measured.
func TestManifestClockOffsets(t *testing.T) {
	fastPoll(t)
	harnessOffsets := func(t *testing.T, dir string) (start, end *float64) {
		h := readHarness(t, dir)
		for field, dst := range map[string]**float64{"clock_offset_start_s": &start, "clock_offset_end_s": &end} {
			if raw, ok := h[field]; ok {
				require.NoError(t, json.Unmarshal(raw, dst), field)
			}
		}
		return start, end
	}

	t.Run("measured", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		skewClock(client, 30*time.Second)
		rec, err := Start(context.Background(), client, HarnessOptions{})
		require.NoError(t, err)
		dir := t.TempDir()
		require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

		var m manifest
		readJSON(t, filepath.Join(dir, "manifest.json"), &m)
		start, end := harnessOffsets(t, dir)
		require.NotNil(t, start)
		require.NotNil(t, end)
		require.InDelta(t, 30, *start, 1)
		run := m.Profiles[UnknownProfile]
		require.Equal(t, start, run.ClockOffsetStartS)
		require.Equal(t, end, run.ClockOffsetEndS)
	})

	t.Run("not measured", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		client.PrependReactor("create", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("forbidden")
		})
		rec, err := Start(context.Background(), client, HarnessOptions{})
		require.NoError(t, err)
		dir := t.TempDir()
		require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

		var raw struct {
			Profiles map[string]map[string]json.RawMessage `json:"profiles"`
		}
		readJSON(t, filepath.Join(dir, "manifest.json"), &raw)
		run := raw.Profiles[UnknownProfile]
		require.NotEmpty(t, run)
		require.NotContains(t, run, "clock_offset_start_s")
		require.NotContains(t, run, "clock_offset_end_s")
		start, end := harnessOffsets(t, dir)
		require.Nil(t, start)
		require.Nil(t, end)
	})
}

// When /version fails, Finish still succeeds and writes manifest.json with tools {}, and logs a
// warning that names the error.
func TestManifestLeavesOutAnUnreadKubernetesVersion(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset()
	buf := captureLog(t)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	client.PrependReactor("get", "version", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("version unavailable")
	})

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

	var m manifest
	readJSON(t, filepath.Join(dir, "manifest.json"), &m)
	require.Equal(t, map[string]string{}, m.Tools)
	log.Flush()
	require.Contains(t, buf.String(), "cannot read the Kubernetes version for manifest.json: version unavailable")
}

// A Finish that stops before writing, on a canceled context or a failed node list, writes no
// manifest.json, as it writes no results.json.
func TestFinishWritesNoManifestWithoutResults(t *testing.T) {
	fastPoll(t)
	t.Run("canceled", func(t *testing.T) {
		rec, err := Start(context.Background(), fake.NewSimpleClientset(), HarnessOptions{})
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		dir := t.TempDir()
		require.ErrorIs(t, rec.Finish(ctx, dir, time.Minute, nil, 1, 0, DefaultBSLDFloor), context.Canceled)
		require.NoFileExists(t, filepath.Join(dir, "manifest.json"))
		require.NoFileExists(t, filepath.Join(dir, "results.json"))
	})
	t.Run("node list fails", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		rec, err := Start(context.Background(), client, HarnessOptions{})
		require.NoError(t, err)
		client.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("nodes unavailable")
		})
		dir := t.TempDir()
		require.ErrorContains(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor), "nodes unavailable")
		require.NoFileExists(t, filepath.Join(dir, "manifest.json"))
		require.NoFileExists(t, filepath.Join(dir, "results.json"))
	})
}

func TestFleetOf(t *testing.T) {
	realNode := func(name, gpus string, labels map[string]string) *corev1.Node {
		n := gpuNode(name, gpus)
		n.Labels = labels
		return n
	}
	cpu := func(name string, labels map[string]string) *corev1.Node {
		n := &corev1.Node{}
		n.Name = name
		n.Labels = labels
		return n
	}
	for _, tc := range []struct {
		name  string
		nodes []*corev1.Node
		keys  []string
		want  fleet
	}{
		{"kwok next to a node without GPUs", []*corev1.Node{kindNode("cp"), kwokNode("n1", "8", nil), kwokNode("n2", "8", nil)}, nil,
			fleet{Mode: fleetKWOK, Nodes: 2, GPUsPerNode: map[int]int{8: 2}, Labels: map[string]int{}}},
		{"real", []*corev1.Node{realNode("n1", "8", nil)}, nil,
			fleet{Mode: fleetReal, Nodes: 1, GPUsPerNode: map[int]int{8: 1}, Labels: map[string]int{}}},
		{"mixed", []*corev1.Node{realNode("n1", "8", nil), kwokNode("n2", "8", nil)}, nil,
			fleet{Mode: fleetMixed, Nodes: 2, GPUsPerNode: map[int]int{8: 2}, Labels: map[string]int{}}},
		{"a node with 0 GPUs counts nowhere, sizes round, and sizes group", []*corev1.Node{realNode("n1", "8", nil), realNode("n2", "8", nil),
			realNode("n3", "3.6", nil), realNode("z", "0", nil)}, nil,
			fleet{Mode: fleetReal, Nodes: 3, GPUsPerNode: map[int]int{8: 2, 4: 1}, Labels: map[string]int{}}},
		{"label counts", []*corev1.Node{
			realNode("n1", "8", map[string]string{"rack": "a", "block": "x"}),
			realNode("n2", "8", map[string]string{"rack": "a", "block": "y"}),
			realNode("n3", "8", map[string]string{"rack": "b"}),
			cpu("c1", map[string]string{"rack": "c", "block": "z", "zone": "q"}),
			realNode("z", "0", map[string]string{"rack": "d"})},
			[]string{"rack", "block", "zone"},
			fleet{Mode: fleetReal, Nodes: 3, GPUsPerNode: map[int]int{8: 3}, Labels: map[string]int{"rack": 2, "block": 2, "zone": 0}}},
		{"no GPU node", []*corev1.Node{kindNode("cp"), cpu("c1", nil)}, []string{"rack"},
			fleet{Nodes: 0, GPUsPerNode: map[int]int{}, Labels: map[string]int{"rack": 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes := make([]corev1.Node, len(tc.nodes))
			for i, n := range tc.nodes {
				nodes[i] = *n
			}
			gpus, _, _, _ := (&Recorder{harness: &harnessStart{}}).gpuCapacity(nodes)
			require.Equal(t, tc.want, fleetOf(nodes, gpus, tc.keys))
		})
	}
}

func TestProviderOf(t *testing.T) {
	withID := func(id string) corev1.Node {
		n := corev1.Node{}
		n.Spec.ProviderID = id
		return n
	}
	for _, tc := range []struct {
		name  string
		nodes []corev1.Node
		want  string
	}{
		{"kind nodes and KWOK nodes without providerID", []corev1.Node{withID("kind://docker/kind/cp"), withID(""), withID("kind://docker/kind/w")}, "kind"},
		{"two providers", []corev1.Node{withID("kind://docker/kind/cp"), withID("aws:///us-east-1a/i-1")}, ""},
		{"no providerID", []corev1.Node{withID(""), withID("")}, ""},
		{"no nodes", nil, ""},
		{"no scheme", []corev1.Node{withID("kind://docker/kind/cp"), withID("i-0123")}, ""},
		{"only a providerID without a scheme", []corev1.Node{withID("i-0123")}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, providerOf(tc.nodes))
		})
	}
}

func TestBuildVCS(t *testing.T) {
	setting := func(k, v string) debug.BuildSetting { return debug.BuildSetting{Key: k, Value: v} }
	commit, dirty := buildVCS(nil)
	require.Empty(t, commit)
	require.Nil(t, dirty)
	commit, dirty = buildVCS(&debug.BuildInfo{Settings: []debug.BuildSetting{setting("vcs.modified", "true")}})
	require.Empty(t, commit, "a stamp without a revision is no stamp")
	require.Nil(t, dirty)

	commit, dirty = buildVCS(&debug.BuildInfo{Settings: []debug.BuildSetting{setting("vcs.revision", "abc"), setting("vcs.modified", "true")}})
	require.Equal(t, "abc", commit)
	require.NotNil(t, dirty)
	require.True(t, *dirty)

	commit, dirty = buildVCS(&debug.BuildInfo{Settings: []debug.BuildSetting{setting("vcs.revision", "abc"), setting("vcs.modified", "false")}})
	require.Equal(t, "abc", commit)
	require.NotNil(t, dirty, "a clean build is written as false")
	require.False(t, *dirty)

	commit, dirty = buildVCS(&debug.BuildInfo{Settings: []debug.BuildSetting{setting("vcs.revision", "abc")}})
	require.Equal(t, "abc", commit)
	require.Nil(t, dirty, "an unknown state is never written as clean")
}

// Finish writes both git fields for a stamped clean build, a stamped dirty build, and neither for a
// build without a stamp.
func TestManifestBuildInfo(t *testing.T) {
	fastPoll(t)
	for _, tc := range []struct {
		name     string
		settings []debug.BuildSetting
		want     map[string]any
	}{
		{"clean", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.modified", Value: "false"}},
			map[string]any{"git_commit": "abc", "git_dirty": false}},
		{"dirty", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.modified", Value: "true"}},
			map[string]any{"git_commit": "abc", "git_dirty": true}},
		{"no VCS stamp", nil, map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubBuildInfo(t, tc.settings...)
			rec, err := Start(context.Background(), fake.NewSimpleClientset(), HarnessOptions{})
			require.NoError(t, err)
			dir := t.TempDir()
			require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

			var raw map[string]any
			readJSON(t, filepath.Join(dir, "manifest.json"), &raw)
			got := map[string]any{}
			for _, k := range []string{"git_commit", "git_dirty"} {
				if v, ok := raw[k]; ok {
					got[k] = v
				}
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRunID(t *testing.T) {
	for _, dir := range []string{"out/kueue-1", "out/kueue-1/"} {
		id, err := runID(dir)
		require.NoError(t, err)
		require.Equal(t, "kueue-1", id, dir)
	}
	wd, err := os.Getwd()
	require.NoError(t, err)
	id, err := runID(".")
	require.NoError(t, err)
	require.Equal(t, filepath.Base(wd), id)
}

// In a directory that already holds a run, a Finish whose events.csv write fails (a directory in its
// place) returns the error and leaves no manifest.json, so the old manifest never sits beside the
// new results.json. A Finish that succeeds replaces it.
func TestFinishRemovesAStaleManifest(t *testing.T) {
	fastPoll(t)
	dir := filepath.Join(t.TempDir(), "new-run")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stale := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(stale, []byte(`{"run_id": "old-run"}`), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "events.csv"), 0o755))

	rec, err := Start(context.Background(), fake.NewSimpleClientset(), HarnessOptions{})
	require.NoError(t, err)
	require.Error(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
	require.FileExists(t, filepath.Join(dir, "results.json"))
	require.NoFileExists(t, stale)

	require.NoError(t, os.Remove(filepath.Join(dir, "events.csv")))
	rec, err = Start(context.Background(), fake.NewSimpleClientset(), HarnessOptions{})
	require.NoError(t, err)
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
	var m manifest
	readJSON(t, stale, &m)
	require.Equal(t, "new-run", m.RunID)
}
