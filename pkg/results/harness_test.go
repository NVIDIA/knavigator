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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	_, _, maxNode, err := rec.gpuCapacity(context.Background())
	require.NoError(t, err)
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
	for _, f := range []string{"results.json", "timeline.csv", "events.csv"} {
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
	for _, f := range []string{"results.json", "timeline.csv", "events.csv"} {
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
	}{
		{"create fails", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("forbidden")
		}},
		// The fake API server leaves CreationTimestamp unset; a zero time is not a measurement.
		{"no creation timestamp", func(a k8stesting.Action) (bool, runtime.Object, error) {
			cm := a.(k8stesting.CreateAction).GetObject().(*corev1.ConfigMap)
			cm.Name = cm.GenerateName + "x"
			return false, nil, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			client.PrependReactor("create", "configmaps", tc.reactor)
			rec, err := Start(context.Background(), client, HarnessOptions{})
			require.NoError(t, err)

			dir := t.TempDir()
			require.NoError(t, rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor), "a clock offset never fails the run")
			h := readHarness(t, dir)
			require.NotContains(t, h, "clock_offset_start_s")
			require.NotContains(t, h, "clock_offset_end_s")
		})
	}
}
