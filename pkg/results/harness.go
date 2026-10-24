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

package results

import (
	"context"
	"fmt"
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

// Harness reports on the cluster under the run, so a run whose measurement cannot be trusted fails.
type Harness struct {
	// ClockOffsetStartSec and ClockOffsetEndSec are the API server's clock minus the host's, in
	// seconds, at the start and end of the run. The server stamps whole seconds.
	ClockOffsetStartSec *float64 `json:"clock_offset_start_s,omitempty"`
	ClockOffsetEndSec   *float64 `json:"clock_offset_end_s,omitempty"`
	// ControlPlaneRestarts lists the containers whose restart count rose during the run.
	ControlPlaneRestarts []Restart `json:"control_plane_restarts"`
}

// Restart is a container that restarted during the run.
type Restart struct {
	Container string `json:"container"` // namespace/pod/container
	Before    int32  `json:"before"`
	After     int32  `json:"after"`
}

// harnessStart is what the checks recorded when the run started.
type harnessStart struct {
	namespaces []string
	restarts   map[string]int32
	clock      *float64
}

// startHarness refuses a run that GPU pods from before it would distort, then records restart
// counts and the clock offset.
func startHarness(ctx context.Context, client kubernetes.Interface, namespaces []string) (*harnessStart, error) {
	if err := checkLeftovers(ctx, client); err != nil {
		return nil, err
	}
	restarts, err := restartCounts(ctx, client, namespaces)
	if err != nil {
		return nil, err
	}
	return &harnessStart{namespaces: namespaces, restarts: restarts, clock: clockOffset(ctx, client)}, nil
}

// finish reads the restart counts and the clock offset again.
func (h *harnessStart) finish(ctx context.Context, client kubernetes.Interface) (Harness, error) {
	after, err := restartCounts(ctx, client, h.namespaces)
	if err != nil {
		return Harness{}, err
	}
	out := Harness{ClockOffsetStartSec: h.clock, ClockOffsetEndSec: clockOffset(ctx, client), ControlPlaneRestarts: []Restart{}}
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

// check fails a run during which a watched container restarted.
func (h Harness) check() error {
	if len(h.ControlPlaneRestarts) == 0 {
		return nil
	}
	parts := make([]string, 0, len(h.ControlPlaneRestarts))
	for _, r := range h.ControlPlaneRestarts {
		parts = append(parts, fmt.Sprintf("%s (%d to %d)", r.Container, r.Before, r.After))
	}
	return fmt.Errorf("containers restarted during the run: %s", strings.Join(parts, ", "))
}

// checkLeftovers fails when GPU pods that have not finished already exist: they hold GPUs the run
// would count as free.
func checkLeftovers(ctx context.Context, client kubernetes.Interface) error {
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}
	var active []string
	for _, p := range pods.Items {
		if podGPUs(&p) == 0 || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		active = append(active, p.Namespace+"/"+p.Name)
	}
	if len(active) == 0 {
		return nil
	}
	sort.Strings(active)
	return fmt.Errorf("%d GPU pods are still active from before the run: %s", len(active), strings.Join(active, ", "))
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

// clockOffset creates a ConfigMap and returns its creation timestamp minus the host time just
// before the create, in seconds, or nil when it cannot measure. Values in (-1, 1) mean the clocks
// agree to the second.
func clockOffset(ctx context.Context, client kubernetes.Interface) *float64 {
	cms := client.CoreV1().ConfigMaps(metav1.NamespaceDefault)
	before := time.Now()
	cm, err := cms.Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{GenerateName: "knavigator-clock-"}}, metav1.CreateOptions{})
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
	offset := cm.CreationTimestamp.Sub(before).Seconds()
	return &offset
}
