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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func gpuPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(name),
			CreationTimestamp: metav1.NewTime(time.Now().Add(time.Hour))},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{gpuResource: resource.MustParse("8")}}}}},
	}
}

func TestFinishStopsWhenStalled(t *testing.T) {
	old := pollInterval
	pollInterval = 20 * time.Millisecond
	defer func() { pollInterval = old }()

	client := fake.NewSimpleClientset()
	rec, err := Start(context.Background(), client)
	require.NoError(t, err)
	// A gang pod that no scheduler ever places.
	_, err = client.CoreV1().Pods("default").Create(context.Background(), gpuPod("g-0"), metav1.CreateOptions{})
	require.NoError(t, err)
	jobs := []Job{{Name: "default/g", Namespace: "default", Submitted: time.Now(), Pods: 1, PodRegexp: "g-[0-9]+"}}

	dir := t.TempDir()
	start := time.Now()
	err = rec.Finish(context.Background(), dir, time.Minute, jobs, 1, 300*time.Millisecond, DefaultBSLDFloor)

	require.EqualError(t, err, "run stalled: 0/1 jobs started")
	require.Less(t, time.Since(start), 10*time.Second)
	data, err := os.ReadFile(filepath.Join(dir, "results.json"))
	require.NoError(t, err)
	var s Summary
	require.NoError(t, json.Unmarshal(data, &s))
	require.True(t, s.Stalled)
	require.Equal(t, 1, s.PodsUnscheduled)
}

func TestProgress(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.Equal(t, 0, progress([]Pod{{Created: t0}}))
	require.Equal(t, 1, progress([]Pod{{Created: t0, Scheduled: t0}}))
	require.Equal(t, 3, progress([]Pod{{Scheduled: t0, Finished: t0}, {Scheduled: t0}}))
}

func TestObserveRecordsPriority(t *testing.T) {
	r := &Recorder{pods: map[types.UID]*Pod{}, events: map[types.UID]*Event{}}
	high, plain := gpuPod("high-0"), gpuPod("plain-0")
	prio := int32(1000)
	high.Spec.Priority = &prio

	r.observe(high, false)
	r.observe(plain, false)

	byName := map[string]int{}
	for _, p := range r.snapshot() {
		byName[p.Name] = p.Priority
	}
	require.Equal(t, map[string]int{"high-0": 1000, "plain-0": 0}, byName, "a pod without a priority reads 0")
}

func TestObserveFailedVictimIsEvicted(t *testing.T) {
	r := &Recorder{pods: map[types.UID]*Pod{}, events: map[types.UID]*Event{}}
	jobs := []Job{{Name: "default/a", Namespace: "default", Pods: 1, PodRegexp: "a-[a-z0-9]+"}}

	victim := gpuPod("a-x1")
	victim.Spec.NodeName = "n1"
	victim.Status.Phase = corev1.PodRunning
	r.observe(victim, false)

	// A real kubelet turns the preempted pod Failed before the API server removes it.
	failed := victim.DeepCopy()
	failed.Status.Phase = corev1.PodFailed
	now := metav1.Now()
	failed.DeletionTimestamp = &now
	failed.Status.Conditions = append(failed.Status.Conditions, corev1.PodCondition{
		Type: corev1.DisruptionTarget, Status: corev1.ConditionTrue, Reason: "PreemptionByScheduler"})
	r.observe(failed, false)
	r.observe(failed, true)

	require.False(t, idle(r.snapshot(), jobs), "the job waits for the victim's replacement")
	s := Summarize(r.snapshot(), jobs, 8, time.Now().Add(2*time.Hour), Options{})
	require.Equal(t, 1, s.PodsEvicted)
	require.Zero(t, s.PodsFailed)

	// A pod that fails on its own is still a failure.
	crashed := gpuPod("a-x2")
	crashed.Spec.NodeName = "n1"
	crashed.Status.Phase = corev1.PodFailed
	r.observe(crashed, false)
	require.Equal(t, 1, Summarize(r.snapshot(), jobs, 8, time.Now().Add(2*time.Hour), Options{}).PodsFailed)
}
