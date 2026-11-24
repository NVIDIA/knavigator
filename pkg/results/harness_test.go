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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	log "k8s.io/klog/v2"
)

func fastPoll(t *testing.T) {
	t.Helper()
	old := pollInterval
	pollInterval = 20 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })
}

func phasePod(namespace, name string, phase corev1.PodPhase, gpus bool) *corev1.Pod {
	p := gpuPod(name)
	p.Namespace = namespace
	p.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Hour))
	p.Status.Phase = phase
	if !gpus {
		p.Spec.Containers[0].Resources.Requests = nil
	}
	return p
}

func containerPod(namespace, name, container string, restarts int32, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: labels},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: container, RestartCount: restarts}}},
	}
}

func setRestarts(t *testing.T, client *fake.Clientset, namespace, name string, restarts int32) {
	t.Helper()
	p, err := client.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err)
	p.Status.ContainerStatuses[0].RestartCount = restarts
	_, err = client.CoreV1().Pods(namespace).Update(context.Background(), p, metav1.UpdateOptions{})
	require.NoError(t, err)
}

// readHarness returns the raw fields of the harness object in results.json.
func readHarness(t *testing.T, dir string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "results.json"))
	require.NoError(t, err)
	var out struct {
		Harness map[string]json.RawMessage `json:"harness"`
	}
	require.NoError(t, json.Unmarshal(data, &out))
	require.NotNil(t, out.Harness, "results.json has a harness object")
	return out.Harness
}

func controlPlane() map[string]string { return map[string]string{"tier": "control-plane"} }

// gpuNode is a node with gpus allocatable GPUs.
func gpuNode(name, gpus string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{gpuResource: resource.MustParse(gpus)}}}
}

// leftoverPod is a GPU pod from before the run, bound to node unless node is empty.
func leftoverPod(namespace, name string, phase corev1.PodPhase, gpus, node string) *corev1.Pod {
	p := phasePod(namespace, name, phase, true)
	p.Spec.Containers[0].Resources.Requests[gpuResource] = resource.MustParse(gpus)
	p.Spec.NodeName = node
	return p
}

// leftoverCluster is one node with 8 GPUs; from before the run, a 2-GPU pod running on it, a
// finished 8-GPU pod and a pod without GPUs; and one pod of the run that held 3 GPUs for exactly an
// hour.
func leftoverCluster() *fake.Clientset {
	return runCluster(leftoverPod("default", "leftover-0", corev1.PodRunning, "2", "n1"),
		phasePod("default", "done-0", corev1.PodSucceeded, true),
		phasePod("default", "web-0", corev1.PodRunning, false))
}

// runCluster is node n1 with 8 GPUs and one pod of the run that held 3 GPUs on it for exactly an
// hour, plus pods.
func runCluster(pods ...*corev1.Pod) *fake.Clientset {
	run := gpuPod("w-0")
	run.Spec.Containers[0].Resources.Requests[gpuResource] = resource.MustParse("3")
	run.Spec.NodeName = "n1"
	placed := run.CreationTimestamp
	run.Status = corev1.PodStatus{
		Phase:      corev1.PodSucceeded,
		Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionTrue, LastTransitionTime: placed}},
		ContainerStatuses: []corev1.ContainerStatus{{Name: "c", State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{FinishedAt: metav1.NewTime(placed.Add(time.Hour))}}}},
	}
	objects := []runtime.Object{gpuNode("n1", "8"), run}
	for _, p := range pods {
		objects = append(objects, p)
	}
	return fake.NewSimpleClientset(objects...)
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog collects klog output for the rest of the test.
func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	out := &syncBuffer{}
	log.LogToStderr(false)
	log.SetOutput(out)
	t.Cleanup(func() {
		log.Flush()
		log.SetOutput(os.Stderr)
		log.LogToStderr(true)
	})
	return out
}

func readSummary(t *testing.T, dir string) Summary {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "results.json"))
	require.NoError(t, err)
	var s Summary
	require.NoError(t, json.Unmarshal(data, &s))
	return s
}

func TestStartRefusesLeftoverGPUPods(t *testing.T) {
	pods := []corev1.Pod{
		*phasePod("team", "a-0", corev1.PodPending, true),
		*phasePod("default", "b-0", corev1.PodRunning, true),
		*phasePod("default", "done-0", corev1.PodSucceeded, true),
		*phasePod("default", "crashed-0", corev1.PodFailed, true),
		*phasePod("default", "web-0", corev1.PodRunning, false),
	}
	client := fake.NewSimpleClientset()
	// The fake lists in name order; this list is out of order, so the error has to sort it. An empty
	// namespace lists all of them, as the API server does.
	client.PrependReactor("list", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		out := &corev1.PodList{}
		for _, p := range pods {
			if ns := a.GetNamespace(); ns == "" || p.Namespace == ns {
				out.Items = append(out.Items, p)
			}
		}
		return true, out, nil
	})

	rec, err := Start(context.Background(), client, HarnessOptions{})

	require.Nil(t, rec)
	require.EqualError(t, err, "2 GPU pods are still active from before the run: default/b-0, team/a-0")
}

func TestStartAcceptsFinishedAndNonGPUPods(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(
		phasePod("default", "done-0", corev1.PodSucceeded, true),
		phasePod("default", "crashed-0", corev1.PodFailed, true),
		phasePod("default", "web-0", corev1.PodRunning, false),
	)

	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
	require.NotContains(t, readHarness(t, dir), "leftover_gpus", "omitted without -results.allow-leftovers")
}

func TestStartRefusesLeftoversByDefault(t *testing.T) {
	rec, err := Start(context.Background(), leftoverCluster(), HarnessOptions{})

	require.Nil(t, rec)
	require.EqualError(t, err, "1 GPU pods are still active from before the run: default/leftover-0")
}

func TestAllowLeftoversSubtractsTheirGPUs(t *testing.T) {
	fastPoll(t)
	logs := captureLog(t)
	rec, err := Start(context.Background(), leftoverCluster(), HarnessOptions{AllowLeftovers: true})
	require.NoError(t, err)
	// The biggest node is a node size for the size-bias metrics, not free capacity.
	list, err := rec.listNodes(context.Background())
	require.NoError(t, err)
	_, _, _, maxNode := rec.gpuCapacity(list)
	require.Equal(t, 8.0, maxNode)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

	// Only the running 2-GPU pod counts; the finished pod and the pod without GPUs do not.
	require.Equal(t, "2", string(readHarness(t, dir)["leftover_gpus"]))
	s := readSummary(t, dir)
	require.Equal(t, 6.0, s.GPUCapacity, "8 GPUs minus the 2 the leftover pod holds")
	// 3 GPUs busy for the whole hour-long run, out of 6.
	require.Equal(t, 0.5, s.GPUUtilization)
	require.Contains(t, logs.String(), "Results: 1 GPU pods are still active from before the run: default/leftover-0; "+
		"counting the 2 GPUs of those bound to a node as busy for the whole run\n")
	require.NotContains(t, logs.String(), "GPU capacity is")
}

func TestAllowLeftoversSkipsUnboundPods(t *testing.T) {
	fastPoll(t)
	for _, tc := range []struct {
		name        string
		pods        []*corev1.Pod
		leftover    string
		capacity    float64
		utilization float64
		warning     string
	}{
		// A pod not yet bound to a node holds no GPUs, whatever it requests: 3 of 8 GPUs for the run.
		{"pending 6", []*corev1.Pod{leftoverPod("team", "queued-0", corev1.PodPending, "6", "")},
			"0", 8, 0.375, "1 GPU pods are still active from before the run: team/queued-0; counting the 0 GPUs"},
		{"pending 8", []*corev1.Pod{leftoverPod("team", "queued-0", corev1.PodPending, "8", "")},
			"0", 8, 0.375, "1 GPU pods are still active from before the run: team/queued-0; counting the 0 GPUs"},
		{"pending 16", []*corev1.Pod{leftoverPod("team", "queued-0", corev1.PodPending, "16", "")},
			"0", 8, 0.375, "1 GPU pods are still active from before the run: team/queued-0; counting the 0 GPUs"},
		{"bound 2 and pending 8", []*corev1.Pod{
			leftoverPod("default", "leftover-0", corev1.PodRunning, "2", "n1"),
			leftoverPod("team", "queued-0", corev1.PodPending, "8", ""),
		}, "2", 6, 0.5, "2 GPU pods are still active from before the run: default/leftover-0, team/queued-0; counting the 2 GPUs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLog(t)
			rec, err := Start(context.Background(), runCluster(tc.pods...), HarnessOptions{AllowLeftovers: true})
			require.NoError(t, err)

			dir := t.TempDir()
			require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

			require.Equal(t, tc.leftover, string(readHarness(t, dir)["leftover_gpus"]))
			s := readSummary(t, dir)
			require.Equal(t, tc.capacity, s.GPUCapacity)
			require.Equal(t, tc.utilization, s.GPUUtilization)
			// Every unfinished pod is named, bound or not.
			require.Contains(t, logs.String(), "Results: "+tc.warning+" of those bound to a node as busy for the whole run\n")
		})
	}
}

func TestAllowLeftoversWarnsWhenNoCapacityIsLeft(t *testing.T) {
	fastPoll(t)
	logs := captureLog(t)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"), leftoverPod("default", "train-0", corev1.PodRunning, "8", "n1"))
	rec, err := Start(context.Background(), client, HarnessOptions{AllowLeftovers: true})
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

	require.Equal(t, 0.0, readSummary(t, dir).GPUCapacity)
	require.Contains(t, logs.String(), "Results: GPU capacity is 0 after subtracting the 8 GPUs that pods from before the run hold; "+
		"gpu_utilization and idle_waiting_gpu_hours read 0\n")
}

func TestAllowLeftoversWithNoneRecordsZero(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(phasePod("default", "done-0", corev1.PodSucceeded, true))
	rec, err := Start(context.Background(), client, HarnessOptions{AllowLeftovers: true})
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
	require.Equal(t, "0", string(readHarness(t, dir)["leftover_gpus"]))
}

func TestAllowLeftoversPushesReducedCapacity(t *testing.T) {
	fastPoll(t)
	srv := &pushed{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	rec, err := Start(context.Background(), leftoverCluster(), HarnessOptions{AllowLeftovers: true})
	require.NoError(t, err)
	rec.PushTo(context.Background(), ts.URL, "r1", 10*time.Millisecond, func() []Job { return nil }, 1)

	// The first interim push, before Finish sends the final one.
	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.value("knavigator_gpu_capacity", nil) != -1
	}, 5*time.Second, 10*time.Millisecond)
	srv.mu.Lock()
	capacity, utilization := srv.value("knavigator_gpu_capacity", nil), srv.value("knavigator_gpu_utilization", nil)
	srv.mu.Unlock()
	require.Equal(t, 6.0, capacity)
	require.Equal(t, 0.5, utilization)

	require.NoError(t, rec.Finish(context.Background(), t.TempDir(), time.Minute, nil, 1, 0, DefaultBSLDFloor))
}

// runPod is a pod of the run that requests gpus, created at created, placed on node at placed and
// finished at finished.
func runPod(name, gpus, node string, created, placed, finished time.Time) *corev1.Pod {
	p := gpuPod(name)
	p.CreationTimestamp = metav1.NewTime(created)
	p.Spec.Containers[0].Resources.Requests[gpuResource] = resource.MustParse(gpus)
	p.Spec.NodeName = node
	p.Status = corev1.PodStatus{
		Phase:      corev1.PodSucceeded,
		Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(placed)}},
		ContainerStatuses: []corev1.ContainerStatus{{Name: "c", State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{FinishedAt: metav1.NewTime(finished)}}}},
	}
	return p
}

// twoNodeCluster is n1 with 8 GPUs and n2 with 4, plus pods. Of the run: a-0 holds 3 GPUs on n1 for
// the whole hour; b-0 asks for 4 at the start, waits 30 minutes and then holds n2 for the rest.
func twoNodeCluster(pods ...*corev1.Pod) *fake.Clientset {
	t0 := time.Now().Add(time.Hour).Truncate(time.Second)
	objects := []runtime.Object{gpuNode("n1", "8"), gpuNode("n2", "4"),
		runPod("a-0", "3", "n1", t0, t0, t0.Add(time.Hour)),
		runPod("b-0", "4", "n2", t0, t0.Add(30*time.Minute), t0.Add(time.Hour))}
	for _, p := range pods {
		objects = append(objects, p)
	}
	return fake.NewSimpleClientset(objects...)
}

// fragmentationOf is the fragmentation part of a summary with its pointers read; -1 for null.
func fragmentationOf(s Summary) []float64 {
	deref := func(f *float64) float64 {
		if f == nil {
			return -1
		}
		return *f
	}
	return []float64{s.FragmentedGPUHours, deref(s.GPUFragmentation), deref(s.GPUFragmentationLargestPod),
		s.FreeNodesMean, s.StrandedGPUHours}
}

func TestAllowLeftoversHoldGPUsOnTheirNode(t *testing.T) {
	fastPoll(t)
	for _, tc := range []struct {
		name     string
		opts     HarnessOptions
		leftover *corev1.Pod
		nodes    map[string]float64
		capacity float64
		// fragmented_gpu_hours, gpu_fragmentation, gpu_fragmentation_largest_pod, free_nodes_mean,
		// stranded_gpu_hours
		fragmentation []float64
		utilization   float64
	}{
		// Free GPUs: n1 has 5 (partly used) all hour; n2 has 4 (empty) for the first 30 minutes.
		// Free 5*60+4*30 = 420 GPU-minutes, fragmented 300, so 5/7. b-0 waits for 4 GPUs, and no
		// node has 1 to 3 free, so nothing is stranded. GPU-hours 3+2 = 5 out of 12.
		{"flag off", HarnessOptions{}, nil,
			map[string]float64{"n1": 8, "n2": 4}, 12, []float64{5, 5.0 / 7, 0, 0.5, 0}, 5.0 / 12},
		// A pod not bound to a node holds nothing: the same numbers as without it.
		{"unbound pending leftover", HarnessOptions{AllowLeftovers: true},
			leftoverPod("team", "queued-0", corev1.PodPending, "2", ""),
			map[string]float64{"n1": 8, "n2": 4}, 12, []float64{5, 5.0 / 7, 0, 0.5, 0}, 5.0 / 12},
		// 2 GPUs held on n1 leave it 3 free, fewer than b-0's 4 and than the largest pod's 4: free
		// 3*60+4*30 = 300 GPU-minutes, fragmented 180 and too small 180, so 0.6 and 0.6. While b-0
		// waits, those 3 are stranded: 3 GPUs for half an hour. GPU-hours 5 out of 10. The node map
		// stays allocatable: the held GPUs count as in use, not as a smaller node.
		{"leftover on n1", HarnessOptions{AllowLeftovers: true},
			leftoverPod("team", "train-0", corev1.PodRunning, "2", "n1"),
			map[string]float64{"n1": 8, "n2": 4}, 10, []float64{3, 0.6, 0.6, 0.5, 1.5}, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pods []*corev1.Pod
			if tc.leftover != nil {
				pods = append(pods, tc.leftover)
			}
			rec, err := Start(context.Background(), twoNodeCluster(pods...), tc.opts)
			require.NoError(t, err)
			list, err := rec.listNodes(context.Background())
			require.NoError(t, err)
			nodes, _, capacity, maxNode := rec.gpuCapacity(list)
			require.Equal(t, tc.nodes, nodes)
			require.Equal(t, tc.capacity, capacity)
			require.Equal(t, 8.0, maxNode, "the biggest node's size, whatever pods hold on it")

			dir := t.TempDir()
			require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
			s := readSummary(t, dir)
			require.Equal(t, tc.capacity, s.GPUCapacity)
			require.InDeltaSlice(t, tc.fragmentation, fragmentationOf(s), 1e-12)
			require.InDelta(t, tc.utilization, s.GPUUtilization, 1e-12)
		})
	}
}

func TestAllowLeftoversFillingANodeLeaveNoFreeNode(t *testing.T) {
	fastPoll(t)
	// Two pods of another user hold 4 GPUs each, all 8 of n2, for the whole run; the run's 3-GPU pod
	// sits on n1.
	client := runCluster(leftoverPod("team", "train-0", corev1.PodRunning, "4", "n2"),
		leftoverPod("team", "train-1", corev1.PodRunning, "4", "n2"))
	_, err := client.CoreV1().Nodes().Create(context.Background(), gpuNode("n2", "8"), metav1.CreateOptions{})
	require.NoError(t, err)
	rec, err := Start(context.Background(), client, HarnessOptions{AllowLeftovers: true})
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
	s := readSummary(t, dir)
	require.Equal(t, 8.0, s.GPUCapacity)
	// The only free GPUs are n1's 5, on a partly used node, for the hour; n2 is never free.
	require.Equal(t, []float64{5, 1, 0, 0, 0}, fragmentationOf(s))
}

func TestAllowLeftoversPartlyHoldAnIdleNode(t *testing.T) {
	fastPoll(t)
	// A 2-GPU pod of another user on n1 for the whole run, and no pod of the run there; the run's
	// 3-GPU pod holds n2 for the hour. n1 has 6 free and n2 has 5, both on partly used nodes, so all
	// 11 free GPUs are fragmented and no node is ever free. Largest pod 3: no node has fewer free.
	t0 := time.Now().Add(time.Hour).Truncate(time.Second)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"), gpuNode("n2", "8"),
		runPod("a-0", "3", "n2", t0, t0, t0.Add(time.Hour)),
		leftoverPod("team", "train-0", corev1.PodRunning, "2", "n1"))
	rec, err := Start(context.Background(), client, HarnessOptions{AllowLeftovers: true})
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
	s := readSummary(t, dir)
	require.Equal(t, 14.0, s.GPUCapacity)
	require.InDelta(t, 3.0/14, s.GPUUtilization, 1e-12)
	require.Equal(t, []float64{11, 1, 0, 0, 0}, fragmentationOf(s))
}

func TestAllowLeftoversOnAVanishedNode(t *testing.T) {
	fastPoll(t)
	for _, gpus := range []string{"8", "2"} {
		t.Run(gpus+" GPUs", func(t *testing.T) {
			// The leftover pod is bound to n2, which is gone at the end of the run. Capacity counts
			// only the nodes there at the end, so neither n2 nor its leftover pod is counted: 8 GPUs
			// of n1, of which the run's pod used 3 for the hour.
			client := runCluster(leftoverPod("team", "train-0", corev1.PodRunning, gpus, "n2"))
			_, err := client.CoreV1().Nodes().Create(context.Background(), gpuNode("n2", "8"), metav1.CreateOptions{})
			require.NoError(t, err)
			rec, err := Start(context.Background(), client, HarnessOptions{AllowLeftovers: true})
			require.NoError(t, err)
			require.NoError(t, client.CoreV1().Nodes().Delete(context.Background(), "n2", metav1.DeleteOptions{}))

			dir := t.TempDir()
			require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
			require.Equal(t, gpus, string(readHarness(t, dir)["leftover_gpus"]), "recorded as found at the start")
			s := readSummary(t, dir)
			require.Equal(t, 8.0, s.GPUCapacity)
			require.Equal(t, 0.375, s.GPUUtilization)
			require.Equal(t, []float64{5, 1, 0, 0, 0}, fragmentationOf(s))
		})
	}
}

func TestAllowLeftoversWarnsWithTheGPUsSubtracted(t *testing.T) {
	fastPoll(t)
	logs := captureLog(t)
	// 8 GPUs held on n1, the only node left at the end, and 2 on n2, which is gone by then: only
	// the 8 are subtracted.
	client := fake.NewSimpleClientset(gpuNode("n1", "8"), gpuNode("n2", "8"),
		leftoverPod("team", "train-0", corev1.PodRunning, "8", "n1"),
		leftoverPod("team", "train-1", corev1.PodRunning, "2", "n2"))
	rec, err := Start(context.Background(), client, HarnessOptions{AllowLeftovers: true})
	require.NoError(t, err)
	require.NoError(t, client.CoreV1().Nodes().Delete(context.Background(), "n2", metav1.DeleteOptions{}))

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
	require.Equal(t, 0.0, readSummary(t, dir).GPUCapacity)
	require.Contains(t, logs.String(), "Results: GPU capacity is 0 after subtracting the 8 GPUs that pods from before the run hold; "+
		"gpu_utilization and idle_waiting_gpu_hours read 0\n")
}

func TestAllowLeftoversPushesPerNodeFragmentation(t *testing.T) {
	fastPoll(t)
	srv := &pushed{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	client := twoNodeCluster(leftoverPod("team", "train-0", corev1.PodRunning, "2", "n1"))
	rec, err := Start(context.Background(), client, HarnessOptions{AllowLeftovers: true})
	require.NoError(t, err)
	rec.PushTo(context.Background(), ts.URL, "r1", 10*time.Millisecond, func() []Job { return nil }, 1)

	// The first interim push, before Finish sends the final one: the same values as the final
	// summary in TestAllowLeftoversHoldGPUsOnTheirNode's "leftover on n1".
	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.value("knavigator_stranded_gpu_hours", nil) != -1
	}, 5*time.Second, 10*time.Millisecond)
	srv.mu.Lock()
	pushed := []float64{srv.value("knavigator_fragmented_gpu_hours", nil), srv.value("knavigator_gpu_fragmentation", nil),
		srv.value("knavigator_gpu_fragmentation_largest_pod", nil), srv.value("knavigator_free_nodes_mean", nil),
		srv.value("knavigator_stranded_gpu_hours", nil)}
	srv.mu.Unlock()
	require.InDeltaSlice(t, []float64{3, 0.6, 0.6, 0.5, 1.5}, pushed, 1e-12)

	require.NoError(t, rec.Finish(context.Background(), t.TempDir(), time.Minute, nil, 1, 0, DefaultBSLDFloor))
}

func TestPushesRackFragmentation(t *testing.T) {
	fastPoll(t)
	srv := &pushed{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	client := twoNodeCluster()
	for _, name := range []string{"n1", "n2"} {
		node, err := client.CoreV1().Nodes().Get(context.Background(), name, metav1.GetOptions{})
		require.NoError(t, err)
		node.Labels = map[string]string{"example.com/rack": "a"}
		_, err = client.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
		require.NoError(t, err)
	}
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	rec.SetRackLabel("example.com/rack")
	rec.PushTo(context.Background(), ts.URL, "r1", 10*time.Millisecond, func() []Job { return nil }, 1)

	// n1 is in use for the whole run, so every free GPU of the rack sits in a rack in use.
	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.value("knavigator_gpu_fragmentation_rack", nil) != -1
	}, 5*time.Second, 10*time.Millisecond)
	srv.mu.Lock()
	require.InDelta(t, 1.0, srv.value("knavigator_gpu_fragmentation_rack", nil), 1e-12)
	srv.mu.Unlock()

	require.NoError(t, rec.Finish(context.Background(), t.TempDir(), time.Minute, nil, 1, 0, DefaultBSLDFloor))
}

func TestFinishFailsOnRestart(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(
		containerPod("kube-system", "kube-scheduler-n1", "kube-scheduler", 0, controlPlane()),
		containerPod("kube-system", "etcd-n1", "etcd", 4, controlPlane()),
		containerPod("kube-system", "kube-proxy-x1", "kube-proxy", 0, nil),
		containerPod("kueue-system", "kueue-controller-manager-a1", "manager", 1, nil),
		containerPod("other", "app-0", "app", 0, nil),
	)
	rec, err := Start(context.Background(), client, HarnessOptions{RestartNamespaces: []string{"kueue-system"}})
	require.NoError(t, err)

	setRestarts(t, client, "kube-system", "kube-scheduler-n1", 1)
	setRestarts(t, client, "kube-system", "kube-proxy-x1", 3)
	setRestarts(t, client, "kueue-system", "kueue-controller-manager-a1", 2)
	setRestarts(t, client, "other", "app-0", 5)

	dir := t.TempDir()
	err = rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor)

	require.EqualError(t, err, "containers restarted during the run: "+
		"kube-system/kube-scheduler-n1/kube-scheduler (0 to 1), "+
		"kueue-system/kueue-controller-manager-a1/manager (1 to 2)")
	// The files are written before the run fails.
	for _, f := range []string{"results.json", "timeline.csv", "events.csv", "manifest.json"} {
		require.FileExists(t, filepath.Join(dir, f))
	}
	var restarts []Restart
	require.NoError(t, json.Unmarshal(readHarness(t, dir)["control_plane_restarts"], &restarts))
	require.Equal(t, []Restart{
		{Container: "kube-system/kube-scheduler-n1/kube-scheduler", Before: 0, After: 1},
		{Container: "kueue-system/kueue-controller-manager-a1/manager", Before: 1, After: 2},
	}, restarts)
}

func TestFinishReportsRestartAndFailedPods(t *testing.T) {
	fastPoll(t)
	failed := gpuPod("f-0")
	failed.Spec.NodeName = "n1"
	failed.Status.Phase = corev1.PodFailed
	client := fake.NewSimpleClientset(
		containerPod("kube-system", "kube-scheduler-n1", "kube-scheduler", 0, controlPlane()),
		failed,
	)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	setRestarts(t, client, "kube-system", "kube-scheduler-n1", 1)

	dir := t.TempDir()
	err = rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor)

	// Neither error hides the other.
	require.EqualError(t, err, "containers restarted during the run: kube-system/kube-scheduler-n1/kube-scheduler (0 to 1)\n"+
		"1 pods failed")
	for _, f := range []string{"results.json", "timeline.csv", "events.csv", "manifest.json"} {
		require.FileExists(t, filepath.Join(dir, f))
	}
}

func TestFinishIgnoresOtherKubeSystemRestarts(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(
		containerPod("kube-system", "kube-scheduler-n1", "kube-scheduler", 0, controlPlane()),
		containerPod("kube-system", "kube-proxy-x1", "kube-proxy", 0, nil),
		containerPod("kube-system", "coredns-c1", "coredns", 2, map[string]string{"k8s-app": "kube-dns"}),
	)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)

	setRestarts(t, client, "kube-system", "kube-proxy-x1", 1)
	setRestarts(t, client, "kube-system", "coredns-c1", 3)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
	require.Equal(t, "[]", string(readHarness(t, dir)["control_plane_restarts"]))
}

func TestFinishIgnoresRestartCountDrop(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(
		containerPod("kube-system", "etcd-n1", "etcd", 4, controlPlane()),
	)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)

	// A count that goes down is not a restart during the run.
	setRestarts(t, client, "kube-system", "etcd-n1", 0)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
	require.Equal(t, "[]", string(readHarness(t, dir)["control_plane_restarts"]))
}

func TestFinishRecordsClockOffset(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset()
	var created []string
	// The fake API server stamps creations 30s ahead of the host and does not generate names.
	client.PrependReactor("create", "configmaps", func(a k8stesting.Action) (bool, runtime.Object, error) {
		cm := a.(k8stesting.CreateAction).GetObject().(*corev1.ConfigMap)
		created = append(created, cm.GenerateName)
		cm.Name = fmt.Sprintf("%s%d", cm.GenerateName, len(created))
		cm.CreationTimestamp = metav1.NewTime(time.Now().Add(30 * time.Second))
		return false, nil, nil
	})
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

	h := readHarness(t, dir)
	for _, field := range []string{"clock_offset_start_s", "clock_offset_end_s"} {
		var offset float64
		require.NoError(t, json.Unmarshal(h[field], &offset), field)
		require.InDelta(t, 30, offset, 1, field)
	}
	require.Equal(t, []string{"knavigator-clock-", "knavigator-clock-"}, created)
	left, err := client.CoreV1().ConfigMaps("default").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, left.Items, "the clock ConfigMaps are deleted")
}

func TestFinishOmitsUnmeasuredClockOffset(t *testing.T) {
	fastPoll(t)
	for _, tc := range []struct {
		name    string
		reactor k8stesting.ReactionFunc
		warning string
	}{
		{"create fails", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("forbidden")
		}, "cannot measure the API server clock: forbidden"},
		// The fake API server leaves CreationTimestamp unset; a zero time is not a measurement.
		{"no creation timestamp", func(a k8stesting.Action) (bool, runtime.Object, error) {
			cm := a.(k8stesting.CreateAction).GetObject().(*corev1.ConfigMap)
			cm.Name = cm.GenerateName + "x"
			return false, nil, nil
		}, "ConfigMap knavigator-clock-x has no creation timestamp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			client.PrependReactor("create", "configmaps", tc.reactor)
			buf := captureLog(t)
			rec, err := Start(context.Background(), client, HarnessOptions{})
			require.NoError(t, err)

			dir := t.TempDir()
			require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor), "a clock offset never fails the run")
			h := readHarness(t, dir)
			require.NotContains(t, h, "clock_offset_start_s")
			require.NotContains(t, h, "clock_offset_end_s")
			log.Flush()
			require.Contains(t, buf.String(), tc.warning)
			require.NotContains(t, buf.String(), "cannot delete ConfigMap", "the ConfigMap is deleted, or was never created")
		})
	}
}

// clockStamp is how the fake API server answers one clock ConfigMap create: offset from the host,
// stamped in whole seconds at the end of a create that takes delay on readClock's host clock, or a
// failed create.
type clockStamp struct {
	offset, delay time.Duration
	fail          bool
}

// stampClock answers the i-th clock ConfigMap create with stamps[i], repeating the last. A delay
// needs standHostClock.
func stampClock(client *fake.Clientset, stamps ...clockStamp) {
	n := 0
	client.PrependReactor("create", "configmaps", func(a k8stesting.Action) (bool, runtime.Object, error) {
		s := stamps[min(n, len(stamps)-1)]
		n++
		if s.fail {
			return true, nil, fmt.Errorf("forbidden")
		}
		if s.delay > 0 {
			later := hostNow().Add(s.delay)
			hostNow = func() time.Time { return later }
		}
		cm := a.(k8stesting.CreateAction).GetObject().(*corev1.ConfigMap)
		cm.Name = fmt.Sprintf("%s%d", cm.GenerateName, n)
		cm.CreationTimestamp = metav1.NewTime(hostNow().Add(s.offset).Truncate(time.Second))
		return false, nil, nil
	})
}

// standHostClock stops readClock's host clock at now until a slow create moves it on.
func standHostClock(t *testing.T, now time.Time) {
	old := hostNow
	hostNow = func() time.Time { return now }
	t.Cleanup(func() { hostNow = old })
}

func TestReadClockBoundsTheOffset(t *testing.T) {
	// Four tenths into a second, so the whole-second stamp reads 0.4s low.
	now := time.Date(2026, 1, 1, 12, 0, 0, 4e8, time.UTC)
	standHostClock(t, now)
	client := fake.NewSimpleClientset()
	skewClock(client, -1000*time.Second)

	c := readClock(context.Background(), client)
	require.NotNil(t, c)
	require.InDelta(t, -1000.4, c.offset, 1e-9)
	require.InDelta(t, -1000.4, c.lo, 1e-9)
	require.InDelta(t, -999.4, c.hi, 1e-9)
	require.Equal(t, now.Add(-1000*time.Second).Truncate(time.Second), c.at)
}

// skewClock stamps the i-th clock ConfigMap offsets[i] from the host, repeating the last.
func skewClock(client *fake.Clientset, offsets ...time.Duration) {
	stamps := make([]clockStamp, len(offsets))
	for i, o := range offsets {
		stamps[i].offset = o
	}
	stampClock(client, stamps...)
}

// between is a clock measurement whose field reads hi - 1 and that puts the true offset between lo
// and hi seconds.
func between(lo, hi float64) *clockReading {
	return &clockReading{offset: hi - 1, lo: lo, hi: hi}
}

// measured is a clock measurement whose field reads offset, from a request that took latency.
func measured(offset, latency float64) *clockReading {
	return &clockReading{offset: offset, lo: offset - latency, hi: offset + 1}
}

func TestSkew(t *testing.T) {
	for _, tc := range []struct {
		name  string
		clock *clockReading
		want  float64
		log   string
	}{
		{name: "not measured"},
		{name: "agrees to the second", clock: between(-0.6, 0.4)},
		{name: "just under the tolerance ahead", clock: between(1.999, 2.999)},
		{name: "at the tolerance ahead", clock: between(2, 3), want: 2.5, log: "the API server clock is 2.0s ahead of the host"},
		{name: "just under the tolerance behind", clock: between(-2.999, -1.999)},
		{name: "at the tolerance behind", clock: between(-3, -2), want: -2.5,
			log: "the API server clock is 3.0s behind the host; reading run times on its clock"},
		{name: "a slow request", clock: between(-0.9, 3.1)},
		{name: "after the host slept", clock: between(-977.6, -976.6), want: -977.1, log: "is 977.6s behind the host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			require.InDelta(t, tc.want, (&harnessStart{clock: tc.clock}).skew().Seconds(), 1e-6)
			log.Flush()
			if tc.log == "" {
				require.NotContains(t, buf.String(), "API server clock")
			} else {
				require.Contains(t, buf.String(), tc.log)
			}
		})
	}
}

func TestCheckFailsWhenTheClockMoves(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end *clockReading
		restarts   []Restart
		want       string
	}{
		{name: "start not measured", end: between(-1, 0)},
		{name: "end not measured", start: between(-978, -977)},
		{name: "just under the tolerance ahead", start: between(-3, -2), end: between(-0.001, 0.999)},
		{name: "at the tolerance ahead", start: between(-3, -2), end: between(0, 1),
			want: "the API server clock moved by +3.0s during the run, so its times are not on one clock"},
		{name: "just under the tolerance behind", start: between(0, 1), end: between(-2.999, -1.999)},
		{name: "at the tolerance behind", start: between(0, 1), end: between(-3, -2),
			want: "the API server clock moved by -3.0s during the run, so its times are not on one clock"},
		{name: "a slow end request", start: between(-0.5, 0.5), end: between(-0.5, 3.5)},
		{name: "reported as the fields read", start: measured(-977.6, 0), end: measured(-1.4, 1.5),
			want: "the API server clock moved by +976.2s during the run, so its times are not on one clock"},
		{name: "and a restart", start: between(-978, -977), end: between(-1.5, -0.5), restarts: []Restart{{Container: "kube-system/etcd-n1/etcd", After: 1}},
			want: "containers restarted during the run: kube-system/etcd-n1/etcd (0 to 1)\n" +
				"the API server clock moved by +976.5s during the run, so its times are not on one clock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Harness{start: tc.start, end: tc.end, ControlPlaneRestarts: tc.restarts}.check()
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.want)
			}
		})
	}
}

func TestSlowClockReadsAreNotSkew(t *testing.T) {
	fastPoll(t)
	for _, tc := range []struct {
		name   string
		stamps []clockStamp
		field  string
		offset float64
	}{
		{"slow start", []clockStamp{{delay: 3 * time.Second}, {}}, "clock_offset_start_s", 2.6},
		{"slow start, stamped as it arrived", []clockStamp{{offset: -3 * time.Second, delay: 3 * time.Second}, {}}, "clock_offset_start_s", -0.4},
		{"slow end", []clockStamp{{}, {delay: 3 * time.Second}}, "clock_offset_end_s", 2.6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			standHostClock(t, time.Now().Truncate(time.Second).Add(4e8))
			client := fake.NewSimpleClientset()
			stampClock(client, tc.stamps...)
			buf := captureLog(t)
			rec, err := Start(context.Background(), client, HarnessOptions{})
			require.NoError(t, err)
			require.Zero(t, rec.offset)

			dir := t.TempDir()
			require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))
			log.Flush()
			require.NotContains(t, buf.String(), "API server clock")
			var offset float64
			require.NoError(t, json.Unmarshal(readHarness(t, dir)[tc.field], &offset))
			require.InDelta(t, tc.offset, offset, 1e-9, "results.json still reports the stamp minus the time before the request")
		})
	}
}

func TestFinishReadsRunTimesOnTheAPIServerClock(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	skewClock(client, -1000*time.Second)
	buf := captureLog(t)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)

	// Submitted on the host; placed 10s later and finished 30s later on the API server's clock.
	submitted := time.Now()
	server := submitted.Add(-1000 * time.Second)
	_, err = client.CoreV1().Pods("default").Create(context.Background(),
		runPod("w-0", "8", "n1", server, server.Add(10*time.Second), server.Add(30*time.Second)), metav1.CreateOptions{})
	require.NoError(t, err)
	jobs := []Job{{Name: "default/w", Namespace: "default", Submitted: submitted, Pods: 1, PodRegexp: "w-[0-9]+"}}

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, 2*time.Second, jobs, 1, 0, DefaultBSLDFloor))
	s := readSummary(t, dir)
	require.Equal(t, 1, s.Pods)
	require.InDelta(t, 10, s.JobWaitSec.Max, 0.6)
	require.InDelta(t, 30, s.MakespanSec, 0.6)
	require.Equal(t, submitted, jobs[0].Submitted, "the caller's jobs keep host time")
	log.Flush()
	var offset float64
	require.NoError(t, json.Unmarshal(readHarness(t, dir)["clock_offset_start_s"], &offset))
	require.Contains(t, buf.String(), fmt.Sprintf("the API server clock is %.1fs behind the host; reading run times on its clock", -offset),
		"the warning and results.json give the same offset")
}

func TestStartTakesTheAPIServerStamp(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	skewClock(client, -1500*time.Millisecond)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	// Created 100ms after Start, stamped in whole seconds by a server 1.5s behind.
	server := time.Now().Add(100*time.Millisecond - 1500*time.Millisecond).Truncate(time.Second)
	_, err = client.CoreV1().Pods("default").Create(context.Background(),
		runPod("w-0", "8", "n1", server, server, server.Add(time.Second)), metav1.CreateOptions{})
	require.NoError(t, err)
	jobs := []Job{{Name: "default/w", Namespace: "default", Submitted: time.Now(), Pods: 1, PodRegexp: "w-[0-9]+"}}

	require.NoError(t, rec.Finish(context.Background(), t.TempDir(), 2*time.Second, jobs, 1, 0, DefaultBSLDFloor))
	require.Len(t, rec.snapshot(), 1)
}

func TestFinishEndsOnTheAPIServerClock(t *testing.T) {
	fastPoll(t)
	for _, tc := range []struct {
		name   string
		stamps []clockStamp
	}{
		{"end measured", []clockStamp{{offset: -1000 * time.Second}}},
		{"end measurement fails", []clockStamp{{offset: -1000 * time.Second}, {fail: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(gpuNode("n1", "8"))
			stampClock(client, tc.stamps...)
			rec, err := Start(context.Background(), client, HarnessOptions{})
			require.NoError(t, err)
			server := time.Now().Add(-1000 * time.Second)
			running := runPod("r-0", "8", "n1", server, server, server)
			running.Status.Phase, running.Status.ContainerStatuses = corev1.PodRunning, nil
			_, err = client.CoreV1().Pods("default").Create(context.Background(), running, metav1.CreateOptions{})
			require.NoError(t, err)
			jobs := []Job{{Name: "default/r", Namespace: "default", Submitted: time.Now(), Pods: 1, PodRegexp: "r-[0-9]+"}}

			dir := t.TempDir()
			require.EqualError(t, rec.Finish(context.Background(), dir, 100*time.Millisecond, jobs, 1, 0, DefaultBSLDFloor),
				"run did not drain: 1/1 jobs started")
			s := readSummary(t, dir)
			require.Equal(t, 1, s.Pods)
			require.InDelta(t, 0.5, s.MakespanSec, 1.5, "the end is on the API server's clock")
			require.GreaterOrEqual(t, s.GPUHours, 0.0)
			require.InDelta(t, 0, s.GPUHours, 8*1.5/3600)
		})
	}
}

// earlyEstimate starts a run 1000s behind, measured at the end of a second, so the recorder's
// estimate of the API server's time reads about 0.45s early; the end measurement fails.
func earlyEstimate(t *testing.T) (*Recorder, *fake.Clientset) {
	fastPoll(t)
	standHostClock(t, time.Now().Truncate(time.Second).Add(950*time.Millisecond))
	client := fake.NewSimpleClientset(gpuNode("n1", "16"))
	stampClock(client, clockStamp{offset: -1000 * time.Second}, clockStamp{fail: true})
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	return rec, client
}

func runningPod(name string, created, placed time.Time) *corev1.Pod {
	p := runPod(name, "8", "n1", created, placed, placed)
	p.Status.Phase, p.Status.ContainerStatuses = corev1.PodRunning, nil
	return p
}

func TestFinishEndsNoEarlierThanAnyTimeRecorded(t *testing.T) {
	for _, tc := range []struct {
		name               string
		pods               func(start time.Time) []*corev1.Pod
		makespan, gpuHours float64
	}{
		{"placed last", func(s time.Time) []*corev1.Pod {
			return []*corev1.Pod{runningPod("r-0", s, s.Add(2*time.Second))}
		}, 2, 0},
		{"created last", func(s time.Time) []*corev1.Pod {
			pending := gpuPod("p-0")
			pending.CreationTimestamp = metav1.NewTime(s.Add(2 * time.Second))
			pending.Status.Phase = corev1.PodPending
			return []*corev1.Pod{runPod("d-0", "8", "n1", s, s, s), pending}
		}, 2, 0},
		{"finished last", func(s time.Time) []*corev1.Pod {
			return []*corev1.Pod{runningPod("r-0", s, s), runPod("d-0", "8", "n1", s, s, s.Add(2*time.Second))}
		}, 2, 8 * 4 / 3600.0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, client := earlyEstimate(t)
			for _, p := range tc.pods(rec.start) {
				_, err := client.CoreV1().Pods("default").Create(context.Background(), p, metav1.CreateOptions{})
				require.NoError(t, err)
			}
			dir := t.TempDir()
			_ = rec.Finish(context.Background(), dir, 100*time.Millisecond, nil, 1, 0, DefaultBSLDFloor)
			s := readSummary(t, dir)
			require.InDelta(t, tc.makespan, s.MakespanSec, 0.01)
			require.InDelta(t, tc.gpuHours, s.GPUHours, 8*0.01/3600)
		})
	}
}

func TestPushCountsARunningPodUntilNow(t *testing.T) {
	fastPoll(t)
	srv := &pushed{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	skewClock(client, -1000*time.Second)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	_, err = client.CoreV1().Pods("default").Create(context.Background(), runningPod("r-0", rec.start, rec.start), metav1.CreateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(rec.snapshot()) == 1 }, 5*time.Second, 10*time.Millisecond)
	rec.PushTo(context.Background(), ts.URL, "r1", 10*time.Millisecond, func() []Job { return nil }, 1)

	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.value("knavigator_gpu_hours", nil) >= 8*2/3600.0
	}, 5*time.Second, 10*time.Millisecond, "the pushed GPU-hours keep growing while the pod runs")
	_ = rec.Finish(context.Background(), t.TempDir(), 100*time.Millisecond, nil, 1, 0, DefaultBSLDFloor)
}

func TestPushEndsNoEarlierThanAnyTimeRecorded(t *testing.T) {
	srv := &pushed{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	rec, client := earlyEstimate(t)
	placed := rec.start.Add(time.Second)
	_, err := client.CoreV1().Pods("default").Create(context.Background(), runningPod("r-0", placed, placed), metav1.CreateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(rec.snapshot()) == 1 }, 5*time.Second, 10*time.Millisecond)
	rec.PushTo(context.Background(), ts.URL, "r1", 10*time.Millisecond, func() []Job { return nil }, 1)

	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.value("knavigator_gpu_hours", nil) != -1
	}, 5*time.Second, 10*time.Millisecond)
	srv.mu.Lock()
	gpuHours := srv.value("knavigator_gpu_hours", nil)
	srv.mu.Unlock()
	require.InDelta(t, 0, gpuHours, 1e-9, "the push ends no earlier than the placement")
	_ = rec.Finish(context.Background(), t.TempDir(), 100*time.Millisecond, nil, 1, 0, DefaultBSLDFloor)
}

func TestFinishEndsNoEarlierThanTimesItTook(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	// Clocks that agree, with an end stamp a little behind, inside the tolerance.
	skewClock(client, 0, -900*time.Millisecond)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	// Placed without a PodScheduled condition, so its placement is the exact time it was seen.
	running := gpuPod("r-0")
	running.CreationTimestamp = metav1.Now()
	running.Spec.NodeName = "n1"
	running.Status.Phase = corev1.PodRunning
	_, err = client.CoreV1().Pods("default").Create(context.Background(), running, metav1.CreateOptions{})
	require.NoError(t, err)
	jobs := []Job{{Name: "default/r", Namespace: "default", Pods: 1, PodRegexp: "r-[0-9]+"}}

	dir := t.TempDir()
	require.EqualError(t, rec.Finish(context.Background(), dir, 100*time.Millisecond, jobs, 1, 0, DefaultBSLDFloor),
		"run did not drain: 1/1 jobs started")
	s := readSummary(t, dir)
	require.GreaterOrEqual(t, s.GPUHours, 8*0.09/3600, "it ran at least through the 100ms wait")
	require.InDelta(t, 8*0.25/3600, s.GPUHours, 8*0.25/3600)
	require.InDelta(t, 0.5, s.MakespanSec, 0.5)
}

func TestFinishEndsOnTheEndStamp(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	// 1000s behind at the start and caught up at the end, with a pod placed after the catch-up.
	skewClock(client, -1000*time.Second, 0)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	placed := time.Now().Truncate(time.Second).Add(-time.Second)
	running := runPod("r-0", "8", "n1", placed, placed, placed)
	running.Status.Phase, running.Status.ContainerStatuses = corev1.PodRunning, nil
	_, err = client.CoreV1().Pods("default").Create(context.Background(), running, metav1.CreateOptions{})
	require.NoError(t, err)
	jobs := []Job{{Name: "default/r", Namespace: "default", Submitted: time.Now(), Pods: 1, PodRegexp: "r-[0-9]+"}}

	dir := t.TempDir()
	err = rec.Finish(context.Background(), dir, 100*time.Millisecond, jobs, 1, 0, DefaultBSLDFloor)
	require.ErrorContains(t, err, "so its times are not on one clock")
	s := readSummary(t, dir)
	// The pod held 8 GPUs from placed to the end stamp, 1 to 2.5 seconds later.
	require.InDelta(t, 8*1.75/3600, s.GPUHours, 8*0.75/3600)
	// The submission moved to the start's clock, 1000s behind the end's: why the run fails.
	require.InDelta(t, 999.5, s.MakespanSec, 1.2)
}

func TestOnServerClockKeepsAnUnknownSubmission(t *testing.T) {
	submitted := time.Now()
	r := &Recorder{offset: -1000 * time.Second}
	jobs := r.onServerClock([]Job{{Name: "a"}, {Name: "b", Submitted: submitted}})
	require.True(t, jobs[0].Submitted.IsZero())
	require.Equal(t, submitted.Add(-1000*time.Second), jobs[1].Submitted)
}

func TestFinishFailsWhenTheClockMovesBack(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	skewClock(client, 0, -1000*time.Second)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	// Stamped after the clock fell 1000s behind, so before the run's start.
	server := time.Now().Add(-1000 * time.Second)
	_, err = client.CoreV1().Pods("default").Create(context.Background(),
		runPod("w-0", "8", "n1", server, server, server.Add(time.Second)), metav1.CreateOptions{})
	require.NoError(t, err)
	jobs := []Job{{Name: "default/w", Namespace: "default", Submitted: time.Now(), Pods: 1, PodRegexp: "w-[0-9]+"}}

	err = rec.Finish(context.Background(), t.TempDir(), 100*time.Millisecond, jobs, 1, 0, DefaultBSLDFloor)
	require.ErrorContains(t, err, "so its times are not on one clock")
	require.ErrorContains(t, err, "run did not drain: 0/1 jobs started")
	require.Empty(t, rec.snapshot())
}

func TestStartFallsBackToTheHostSecond(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	stampClock(client, clockStamp{fail: true}, clockStamp{})
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	// Stamped in the same second the run started.
	now := time.Now().Truncate(time.Second)
	_, err = client.CoreV1().Pods("default").Create(context.Background(),
		runPod("w-0", "8", "n1", now, now, now), metav1.CreateOptions{})
	require.NoError(t, err)
	jobs := []Job{{Name: "default/w", Namespace: "default", Submitted: now, Pods: 1, PodRegexp: "w-[0-9]+"}}

	require.NoError(t, rec.Finish(context.Background(), t.TempDir(), 2*time.Second, jobs, 1, 0, DefaultBSLDFloor))
	require.Len(t, rec.snapshot(), 1)
	require.Zero(t, rec.start.Nanosecond(), "the host time, truncated like a stamp")
	require.WithinDuration(t, time.Now(), rec.start, 2*time.Second)
}

func TestPushToReadsRunTimesOnTheAPIServerClock(t *testing.T) {
	fastPoll(t)
	srv := &pushed{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	skewClock(client, -1000*time.Second)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	// Submitted 10s before it is placed, and still running.
	submitted := time.Now().Add(-10 * time.Second)
	server := time.Now().Add(-1000 * time.Second)
	running := runPod("w-0", "8", "n1", server, server, server)
	running.Status.Phase, running.Status.ContainerStatuses = corev1.PodRunning, nil
	_, err = client.CoreV1().Pods("default").Create(context.Background(), running, metav1.CreateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(rec.snapshot()) == 1 }, 5*time.Second, 10*time.Millisecond)
	jobs := []Job{{Name: "default/w", Namespace: "default", Submitted: submitted, Pods: 1, PodRegexp: "w-[0-9]+"}}
	rec.PushTo(context.Background(), ts.URL, "r1", 10*time.Millisecond, func() []Job { return jobs }, 1)

	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.value("knavigator_makespan_sec", nil) != -1
	}, 5*time.Second, 10*time.Millisecond)
	srv.mu.Lock()
	makespan, wait := srv.value("knavigator_makespan_sec", nil), srv.value("knavigator_job_wait_sec", map[string]string{"stat": "max"})
	srv.mu.Unlock()
	require.InDelta(t, 10.5, makespan, 1)
	require.InDelta(t, 10, wait, 0.6)
	require.Error(t, rec.Finish(context.Background(), t.TempDir(), 100*time.Millisecond, jobs, 1, 0, DefaultBSLDFloor))
}

func TestStartLeavesOutPodsFromBeforeTheRun(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	skewClock(client, -400*time.Millisecond)
	buf := captureLog(t)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	before := runPod("old-0", "8", "n1", time.Now().Add(-2*time.Second), time.Now(), time.Now())
	_, err = client.CoreV1().Pods("default").Create(context.Background(), before, metav1.CreateOptions{})
	require.NoError(t, err)

	require.NoError(t, rec.Finish(context.Background(), t.TempDir(), time.Minute, nil, 1, 0, DefaultBSLDFloor))
	log.Flush()
	require.NotContains(t, buf.String(), "API server clock")
	require.Empty(t, rec.snapshot())
	require.Zero(t, rec.offset)
}

func TestFinishFailsWhenTheClockMoves(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(gpuNode("n1", "8"))
	// 1000s behind at the start, then caught up before the first pod.
	skewClock(client, -1000*time.Second, 0)
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	now := time.Now()
	_, err = client.CoreV1().Pods("default").Create(context.Background(),
		runPod("w-0", "8", "n1", now, now.Add(10*time.Second), now.Add(30*time.Second)), metav1.CreateOptions{})
	require.NoError(t, err)
	jobs := []Job{{Name: "default/w", Namespace: "default", Submitted: now, Pods: 1, PodRegexp: "w-[0-9]+"}}

	dir := t.TempDir()
	err = rec.Finish(context.Background(), dir, 2*time.Second, jobs, 1, 0, DefaultBSLDFloor)
	require.Error(t, err)
	require.Regexp(t, `^the API server clock moved by \+(999|1000)\.\ds during the run, so its times are not on one clock$`, err.Error())
	require.Equal(t, 1, readSummary(t, dir).Pods, "the files are written first")
}

// imagePod is a pod with one container status per image/ID pair, and one init container status.
func imagePod(namespace, name string, labels map[string]string, images ...[2]string) *corev1.Pod {
	p := containerPod(namespace, name, "c", 0, labels)
	p.Status.ContainerStatuses = nil
	for i, im := range images {
		p.Status.ContainerStatuses = append(p.Status.ContainerStatuses,
			corev1.ContainerStatus{Name: fmt.Sprintf("c%d", i), Image: im[0], ImageID: im[1]})
	}
	p.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "init", Image: "init:1", ImageID: "sha256:init"}}
	return p
}

func TestImagesOf(t *testing.T) {
	pods := []corev1.Pod{
		*imagePod("ns", "a", nil, [2]string{"app:1", "sha256:bb"}, [2]string{"side:1", "sha256:ss"}),
		// The same image reference under two IDs, such as a tag that moved, lists both, sorted.
		*imagePod("ns", "b", nil, [2]string{"app:1", "sha256:aa"}),
		*imagePod("ns", "c", nil, [2]string{"app:1", "sha256:bb"}),
		// An empty ID, as for an image not pulled yet, adds no ID and does not hide the others.
		*imagePod("ns", "d", nil, [2]string{"app:1", ""}, [2]string{"pending:1", ""}),
	}
	require.Equal(t, map[string]string{
		"app:1":     "sha256:aa,sha256:bb",
		"side:1":    "sha256:ss",
		"pending:1": "",
	}, imagesOf(pods), "init containers are left out")
	require.Equal(t, map[string]string{}, imagesOf(nil))
	slices.Reverse(pods)
	require.Equal(t, "sha256:aa,sha256:bb", imagesOf(pods)["app:1"])
}

// manifest.json lists the images of the control-plane pods and of the pods in
// -results.restart-namespaces, and of no pod in another namespace.
func TestManifestImagesCoverWatchedPods(t *testing.T) {
	fastPoll(t)
	client := fake.NewSimpleClientset(
		imagePod("kube-system", "kube-scheduler-n1", controlPlane(), [2]string{"scheduler:1", "sha256:11"}),
		imagePod("kube-system", "kube-proxy-x1", nil, [2]string{"proxy:1", "sha256:22"}),
		imagePod("kueue-system", "manager", nil, [2]string{"kueue:1", "sha256:33"}),
		imagePod("other", "app-0", nil, [2]string{"app:1", "sha256:44"}),
	)
	rec, err := Start(context.Background(), client, HarnessOptions{RestartNamespaces: []string{"kueue-system"}})
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

	var m manifest
	readJSON(t, filepath.Join(dir, "manifest.json"), &m)
	require.Equal(t, map[string]string{"scheduler:1": "sha256:11", "kueue:1": "sha256:33"}, m.Profiles[UnknownProfile].Images)
}
