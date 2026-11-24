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
	"fmt"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	log "k8s.io/klog/v2"
)

// RunInfo is what a run ran, which the recorder cannot see on the cluster. manifest.json records it
// so that runs pooled later are never mixed with runs that differ.
type RunInfo struct {
	// Profile names the scheduler setup under test. It keys the run's entry under profiles in
	// manifest.json; empty gives UnknownProfile.
	Profile string
	// TopologyLabels are node label keys, besides the rack label, whose distinct values over the GPU
	// nodes manifest.json counts in fleet.labels.
	TopologyLabels []string
	// Workflows are the workflows in the order they ran; a workflow given twice is listed twice.
	Workflows []Workflow
	// Vars are the -var values the workflows used.
	Vars map[string]string
}

// Workflow is one workflow of the run, with the digests of the files it read.
type Workflow struct {
	// Path is the file the workflow was read from.
	Path string `json:"path"`
	// Digest is "sha256:<hex>" of the workflow's text after -var substitution.
	Digest string `json:"digest"`
	// Templates maps each template file that the workflow's RegisterObj tasks read to the digest of
	// its text.
	Templates map[string]string `json:"templates"`
}

// UnknownProfile keys the entry of a run whose profile was not given.
const UnknownProfile = "unknown"

// SetRunInfo records what the run ran, for manifest.json. Without it the run is filed under
// UnknownProfile with an empty workload.
func (r *Recorder) SetRunInfo(info RunInfo) {
	r.info = info
}

// readBuildInfo is the binary's build info; a variable so tests can give it VCS settings, which a
// test binary never has.
var readBuildInfo = debug.ReadBuildInfo

// manifest is the content of manifest.json, which says what ran and where. The top-level fields
// and the profile entries follow the JSON names and types of the Scheduling Capabilities Suite's
// manifest (pkg/report/manifest.go), so the Suite's readers accept the file. The Suite keeps fleet
// as raw JSON and writes a list of nodes in it, where knavigator writes its own summary with a node
// count. workload is knavigator's own, and the Suite drops it.
//
// Every map is written, as {} when empty, so two manifests compare without null checks.
type manifest struct {
	Schema int    `json:"schema"`
	RunID  string `json:"run_id"`
	// GitCommit and GitDirty come from the binary's VCS stamp, and are left out together when it has
	// none, as with go run or a test binary.
	GitCommit string                `json:"git_commit,omitempty"`
	GitDirty  *bool                 `json:"git_dirty,omitempty"`
	Tools     map[string]string     `json:"tools"`
	Fleet     fleet                 `json:"fleet"`
	Profiles  map[string]profileRun `json:"profiles"`
	Workload  workload              `json:"workload"`
}

// manifestSchema is the Suite's manifest schema version.
const manifestSchema = 1

// fleet summarizes the GPU nodes at the end of the run. A GPU node has more than 0 allocatable
// nvidia.com/gpu, as for nodes_by_free_gpus in results.json. Nodes without GPUs,
// such as kind's control-plane node, are left out, or every kind cluster with KWOK nodes would
// read mixed.
type fleet struct {
	// Mode is left out when there is no GPU node.
	Mode  fleetMode `json:"mode,omitempty"`
	Nodes int       `json:"nodes"`
	// GPUsPerNode counts the nodes of each size, rounded as nodes_by_free_gpus rounds them.
	GPUsPerNode map[int]int `json:"gpus_per_node"`
	// Labels counts the distinct values of each rack and topology label key over the nodes that
	// have it; a key no GPU node has counts 0, so a run with a declared but missing level differs
	// from a run that declared none.
	Labels map[string]int `json:"labels"`
}

// fleetMode says whether the GPU nodes are KWOK nodes, real nodes, or both.
type fleetMode string

const (
	fleetKWOK  fleetMode = "kwok"
	fleetReal  fleetMode = "real"
	fleetMixed fleetMode = "mixed"
)

// kwokNodeAnnotation marks a node that KWOK fakes, with the value "fake", as the virtual-nodes chart
// and the Suite's fleet set it.
const kwokNodeAnnotation = "kwok.x-k8s.io/node"

// profileRun is the run's entry under its profile.
type profileRun struct {
	// Start and End are when the recorder started and stopped, in UTC. Start is on the API server's
	// clock, or on the host's when Start cannot measure it. End is no earlier than the last time
	// recorded, and pod times are on the API server's clock, so End can be on a different clock than
	// Start. results.json measures from the first pod or job, which can be later than Start.
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// Cluster is the provider named by the nodes' spec.providerID, such as kind; left out unless
	// every node that has one names the same provider.
	Cluster string `json:"cluster,omitempty"`
	// Images maps each image reference that a watched pod runs to the image ID it resolved to.
	Images map[string]string `json:"images"`
	// The clock offsets are results.json's harness readings, left out when not measured.
	ClockOffsetStartS *float64 `json:"clock_offset_start_s,omitempty"`
	ClockOffsetEndS   *float64 `json:"clock_offset_end_s,omitempty"`
	WatchErrors       int      `json:"watch_errors"`
}

// workload is what the run submitted.
type workload struct {
	Workflows []Workflow        `json:"workflows"`
	Vars      map[string]string `json:"vars"`
	// Speedup is -speedup, written even at 1, unlike results.json's speedup.
	Speedup float64 `json:"speedup"`
	// BSLDFloorSec is -results.bsld-floor in seconds of simulated time. The bsld values in
	// results.json depend on it.
	BSLDFloorSec float64 `json:"bsld_floor_sec"`
	// AllowLeftovers is -results.allow-leftovers. With it, gpu_capacity in results.json excludes the
	// GPUs that pods from before the run hold.
	AllowLeftovers bool `json:"allow_leftovers"`
}

// manifest describes the run for manifest.json. Finish passes what it read at the end of the run,
// so manifest.json and results.json describe the same nodes, window and harness readings. It reads
// the API server's version itself, and leaves it out with a warning when it cannot, since a
// missing version must not lose the run. Finish calls it before writing any file, so its one
// error, a working directory that runID cannot read, leaves no file behind.
func (r *Recorder) manifest(ctx context.Context, dir string, end time.Time, nodes []corev1.Node, gpus map[string]float64, harness Harness, images map[string]string, speedup float64, bsldFloor time.Duration) (manifest, error) {
	id, err := runID(dir)
	if err != nil {
		return manifest{}, err
	}
	tools := map[string]string{}
	if v, err := r.client.Discovery().ServerVersionWithContext(ctx); err != nil {
		log.Warningf("Results: cannot read the Kubernetes version for manifest.json: %v", err)
	} else {
		tools["kubernetes"] = v.GitVersion
	}
	info, _ := readBuildInfo()
	commit, dirty := buildVCS(info)
	keys := slices.Clone(r.info.TopologyLabels)
	if r.rackLabel != "" {
		keys = append(keys, r.rackLabel)
	}
	missed := r.watch.count()
	if missed > 0 {
		log.Warningf("Results: the pod or event watch failed or relisted %d times during the run, so the recorder may have missed changes; manifest.json records it as watch_errors", missed)
	}
	profile := r.info.Profile
	if profile == "" {
		profile = UnknownProfile
	}
	return manifest{
		Schema:    manifestSchema,
		RunID:     id,
		GitCommit: commit,
		GitDirty:  dirty,
		Tools:     tools,
		Fleet:     fleetOf(nodes, gpus, keys),
		Profiles: map[string]profileRun{profile: {
			Start:             r.start.UTC(),
			End:               end.UTC(),
			Cluster:           providerOf(nodes),
			Images:            nonNil(images),
			ClockOffsetStartS: harness.ClockOffsetStartSec,
			ClockOffsetEndS:   harness.ClockOffsetEndSec,
			WatchErrors:       missed,
		}},
		Workload: workload{Workflows: workflowsOrEmpty(r.info.Workflows), Vars: nonNil(r.info.Vars), Speedup: speedup,
			BSLDFloorSec: bsldFloor.Seconds(),
			// The harness records leftover GPUs only under -results.allow-leftovers.
			AllowLeftovers: harness.LeftoverGPUs != nil},
	}, nil
}

// runID is the name of the results directory, as the Suite names a run by its run directory. The
// path is made absolute first, so -results . names the directory and not ".".
func runID(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("cannot name the run from %q: %w", dir, err)
	}
	return filepath.Base(abs), nil
}

// buildVCS reads the commit and the modified flag that go build stamps from the repository. Both
// are empty when info is nil or has no vcs.revision.
func buildVCS(info *debug.BuildInfo) (commit string, dirty *bool) {
	if info == nil {
		return "", nil
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
		case "vcs.modified":
			modified := s.Value == "true"
			dirty = &modified
		}
	}
	if commit == "" {
		return "", nil
	}
	return commit, dirty
}

// fleetOf summarizes the GPU nodes, counting the distinct values of each of labelKeys. gpus is the
// map gpuCapacity built from nodes, so the fleet and gpu_capacity in results.json read GPUs by one
// rule; nodes gives the annotations and labels of those nodes.
func fleetOf(nodes []corev1.Node, gpus map[string]float64, labelKeys []string) fleet {
	// gpuCapacity also keeps nodes with 0 GPUs.
	gpu := map[string]float64{}
	for name, g := range gpus {
		if g > 0 {
			gpu[name] = g
		}
	}
	f := fleet{Nodes: len(gpu), GPUsPerNode: sizeCounts(gpu), Labels: map[string]int{}}
	values := map[string]map[string]bool{}
	for _, key := range labelKeys {
		values[key] = map[string]bool{}
	}
	var kwok int
	for _, node := range nodes {
		if _, ok := gpu[node.Name]; !ok {
			continue
		}
		if node.Annotations[kwokNodeAnnotation] == "fake" {
			kwok++
		}
		for key, set := range values {
			if v, ok := node.Labels[key]; ok {
				set[v] = true
			}
		}
	}
	for key, set := range values {
		f.Labels[key] = len(set)
	}
	switch {
	case len(gpu) == 0:
	case kwok == len(gpu):
		f.Mode = fleetKWOK
	case kwok == 0:
		f.Mode = fleetReal
	default:
		f.Mode = fleetMixed
	}
	return f
}

// providerOf is the provider name before "://" in the nodes' spec.providerID, when every node that
// has one names the same provider; empty otherwise. Kubernetes defines providerID as
// <ProviderName>://<ProviderSpecificNodeID>, and kind sets kind://<provider>/<cluster>/<node>, while
// KWOK nodes set none, so a kind cluster with KWOK GPU nodes reads kind. A providerID without "://"
// names no provider, so it makes the result empty.
func providerOf(nodes []corev1.Node) string {
	var provider string
	for _, node := range nodes {
		id := node.Spec.ProviderID
		if id == "" {
			continue
		}
		name, _, ok := strings.Cut(id, "://")
		if !ok || name == "" || (provider != "" && name != provider) {
			return ""
		}
		provider = name
	}
	return provider
}

// workflowsOrEmpty gives an empty list for a nil one, so manifest.json writes [] and not null.
func workflowsOrEmpty(w []Workflow) []Workflow {
	if w == nil {
		return []Workflow{}
	}
	return w
}

// nonNil gives an empty map for a nil one, so manifest.json writes {} and not null.
func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
