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
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	log "k8s.io/klog/v2"
)

// Static control-plane pods carry this label on kubeadm and kind. Other kube-system pods, such as
// kube-proxy and coredns, can restart without invalidating a scheduling measurement.
const controlPlaneSelector = "tier=control-plane"

// HarnessOptions configures the checks Start runs on the cluster under the run.
type HarnessOptions struct {
	// RestartNamespaces are watched for container restarts, besides the control-plane pods.
	RestartNamespaces []string
	// AllowLeftovers lets the run start while GPU pods from before it are active. The GPUs of those
	// bound to a node are subtracted from capacity, as if they were held for the whole run.
	AllowLeftovers bool
}

// Harness reports on the cluster under the run, so a run whose measurement cannot be trusted fails.
type Harness struct {
	// ClockOffsetStartSec and ClockOffsetEndSec are the API server's clock minus the host's, in
	// seconds, at the start and end of the run. The server stamps whole seconds.
	ClockOffsetStartSec *float64 `json:"clock_offset_start_s,omitempty"`
	ClockOffsetEndSec   *float64 `json:"clock_offset_end_s,omitempty"`
	// LeftoverGPUs are the GPUs held by pods from before the run that are bound to a node, set with
	// AllowLeftovers.
	LeftoverGPUs *float64 `json:"leftover_gpus,omitempty"`
	// ControlPlaneRestarts lists the containers whose restart count rose during the run.
	ControlPlaneRestarts []Restart `json:"control_plane_restarts"`
	start, end           *clockReading
}

// clockReading is one measurement of the API server clock against the host's.
type clockReading struct {
	// offset is the stamp minus the host time just before the request, as results.json reports it.
	offset float64
	// lo and hi bound the true offset, allowing for request latency and one-second stamps.
	lo, hi float64
	at     time.Time
}

func (c *clockReading) mid() float64 {
	return (c.lo + c.hi) / 2
}

func (c *clockReading) seconds() *float64 {
	if c == nil {
		return nil
	}
	return &c.offset
}

// Restart is a container that restarted during the run.
type Restart struct {
	Container string `json:"container"` // namespace/pod/container
	Before    int32  `json:"before"`
	After     int32  `json:"after"`
}

// harnessStart is what the checks recorded when the run started.
type harnessStart struct {
	namespaces   []string
	restarts     map[string]int32
	clock        *clockReading
	leftoverGPUs *float64
	// leftoverByNode is leftoverGPUs split by the node the pods are bound to.
	leftoverByNode map[string]float64
}

// startHarness refuses a run that GPU pods from before it would distort, unless opts allows them,
// then records restart counts and the clock offset.
func startHarness(ctx context.Context, client kubernetes.Interface, opts HarnessOptions) (*harnessStart, error) {
	gpus, byNode, err := checkLeftovers(ctx, client, opts.AllowLeftovers)
	if err != nil {
		return nil, err
	}
	restarts, err := restartCounts(ctx, client, opts.RestartNamespaces)
	if err != nil {
		return nil, err
	}
	h := &harnessStart{namespaces: opts.RestartNamespaces, restarts: restarts, clock: readClock(ctx, client)}
	if opts.AllowLeftovers {
		h.leftoverGPUs = &gpus
		h.leftoverByNode = byNode
	}
	return h, nil
}

// clockSkewTolerance, in seconds, allows for request latency and one-second stamps.
const clockSkewTolerance = 2.0

// hostNow is the host clock readClock reads; a variable so tests can stand it still.
var hostNow = time.Now

// skew returns the API server clock minus the host's, logging it, or 0 within the tolerance.
func (h *harnessStart) skew() time.Duration {
	c := h.clock
	if c == nil || (c.lo < clockSkewTolerance && c.hi > -clockSkewTolerance) {
		return 0
	}
	side := "ahead of"
	if c.offset < 0 {
		side = "behind"
	}
	log.Warningf("Results: the API server clock is %.1fs %s the host; reading run times on its clock", math.Abs(c.offset), side)
	return time.Duration(c.mid() * float64(time.Second))
}

// finish reads the restart counts and the clock offset again.
func (h *harnessStart) finish(ctx context.Context, client kubernetes.Interface) (Harness, error) {
	after, err := restartCounts(ctx, client, h.namespaces)
	if err != nil {
		return Harness{}, err
	}
	end := readClock(ctx, client)
	out := Harness{ClockOffsetStartSec: h.clock.seconds(), ClockOffsetEndSec: end.seconds(), LeftoverGPUs: h.leftoverGPUs,
		ControlPlaneRestarts: []Restart{}, start: h.clock, end: end}
	for c, before := range h.restarts {
		if n, ok := after[c]; ok && n > before {
			out.ControlPlaneRestarts = append(out.ControlPlaneRestarts, Restart{Container: c, Before: before, After: n})
		}
	}
	sort.Slice(out.ControlPlaneRestarts, func(i, j int) bool {
		return out.ControlPlaneRestarts[i].Container < out.ControlPlaneRestarts[j].Container
	})
	return out, nil
}

// check fails a run during which a watched container restarted or the API server clock moved
// against the host's, since then its times are not on one clock.
func (h Harness) check() error {
	var errs []error
	if len(h.ControlPlaneRestarts) > 0 {
		parts := make([]string, 0, len(h.ControlPlaneRestarts))
		for _, r := range h.ControlPlaneRestarts {
			parts = append(parts, fmt.Sprintf("%s (%d to %d)", r.Container, r.Before, r.After))
		}
		errs = append(errs, fmt.Errorf("containers restarted during the run: %s", strings.Join(parts, ", ")))
	}
	if s, e := h.start, h.end; s != nil && e != nil && (s.lo-e.hi >= clockSkewTolerance || e.lo-s.hi >= clockSkewTolerance) {
		errs = append(errs, fmt.Errorf("the API server clock moved by %+.1fs during the run, so its times are not on one clock", e.offset-s.offset))
	}
	return errors.Join(errs...)
}

// checkLeftovers fails when GPU pods that have not finished already exist: they hold GPUs the run
// would count as free. With allow it logs them instead and returns the GPUs requested by those bound
// to a node, in total and per node.
func checkLeftovers(ctx context.Context, client kubernetes.Interface, allow bool) (float64, map[string]float64, error) {
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, nil, fmt.Errorf("failed to list pods: %w", err)
	}
	var active []string
	var gpus float64
	byNode := map[string]float64{}
	for _, p := range pods.Items {
		n := podGPUs(&p)
		if n == 0 || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		active = append(active, p.Namespace+"/"+p.Name)
		// A pod not yet bound to a node holds no GPUs.
		if p.Spec.NodeName != "" {
			gpus += n
			byNode[p.Spec.NodeName] += n
		}
	}
	if len(active) == 0 {
		return 0, byNode, nil
	}
	sort.Strings(active)
	msg := fmt.Sprintf("%d GPU pods are still active from before the run: %s", len(active), strings.Join(active, ", "))
	if !allow {
		return 0, nil, errors.New(msg)
	}
	log.Warningf("Results: %s; counting the %g GPUs of those bound to a node as busy for the whole run", msg, gpus)
	return gpus, byNode, nil
}

// restartCounts maps namespace/pod/container to its restart count, for the control-plane pods and
// every pod in namespaces.
func restartCounts(ctx context.Context, client kubernetes.Interface, namespaces []string) (map[string]int32, error) {
	out := map[string]int32{}
	add := func(namespace, selector string) error {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return fmt.Errorf("failed to list pods in %s: %w", namespace, err)
		}
		for _, p := range pods.Items {
			for _, c := range p.Status.ContainerStatuses {
				out[p.Namespace+"/"+p.Name+"/"+c.Name] = c.RestartCount
			}
		}
		return nil
	}
	if err := add(metav1.NamespaceSystem, controlPlaneSelector); err != nil {
		return nil, err
	}
	for _, ns := range namespaces {
		if err := add(ns, ""); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// readClock times a ConfigMap create against the host clock, or returns nil when it cannot.
func readClock(ctx context.Context, client kubernetes.Interface) *clockReading {
	cms := client.CoreV1().ConfigMaps(metav1.NamespaceDefault)
	before := hostNow()
	cm, err := cms.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{GenerateName: "knavigator-clock-"}}, metav1.CreateOptions{})
	after := hostNow()
	if err != nil {
		log.Warningf("Results: cannot measure the API server clock: %v", err)
		return nil
	}
	if err := cms.Delete(ctx, cm.Name, metav1.DeleteOptions{}); err != nil {
		log.Warningf("Results: cannot delete ConfigMap %s/%s: %v", cm.Namespace, cm.Name, err)
	}
	if cm.CreationTimestamp.IsZero() {
		log.Warningf("Results: cannot measure the API server clock: ConfigMap %s has no creation timestamp", cm.Name)
		return nil
	}
	at := cm.CreationTimestamp.Time
	return &clockReading{offset: at.Sub(before).Seconds(), lo: at.Sub(after).Seconds(), hi: at.Add(time.Second).Sub(before).Seconds(), at: at}
}
