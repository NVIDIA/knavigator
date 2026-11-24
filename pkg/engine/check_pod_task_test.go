/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
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

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/NVIDIA/knavigator/pkg/config"
)

func TestCheckPodParams(t *testing.T) {
	taskID := "check"
	testCases := []struct {
		name       string
		simClients bool
		params     map[string]interface{}
		refTaskId  string
		err        string
		task       *CheckPodTask
	}{
		{
			name:   "Case 1a: no k8s client",
			params: nil,
			err:    "CheckPod/check: Kubernetes client is not set",
		},
		{
			name:       "Case 1b: no parameters map",
			simClients: true,
			params:     nil,
			err:        "CheckPod/check: missing parameter 'refTaskId'",
		},
		{
			name:       "Case 2: invalid input",
			simClients: true,
			params: map[string]interface{}{
				"refTaskId": []string{"1000"},
			},
			err: "CheckPod/check: failed to parse parameters: yaml: unmarshal errors:\n  line 2: cannot unmarshal !!seq into string",
		},
		{
			name:       "Case 3a: missing state",
			simClients: true,
			params: map[string]interface{}{
				"refTaskId": "step1",
			},
			err: "CheckPod/check: missing parameters 'status', 'nodeLabels' and/or 'nodeLabelDomains'",
		},
		{
			name:       "Case 3b: nodeLabelDomains without a key",
			simClients: true,
			params: map[string]interface{}{
				"refTaskId":        "step1",
				"status":           "Running",
				"nodeLabelDomains": map[string]interface{}{"max": 1},
			},
			err: "CheckPod/check: parameter 'nodeLabelDomains.key' must be set",
		},
		{
			name:       "Case 3c: nodeLabelDomains without max",
			simClients: true,
			params: map[string]interface{}{
				"refTaskId":        "step1",
				"nodeLabelDomains": map[string]interface{}{"key": rackLabel},
			},
			err: "CheckPod/check: parameter 'nodeLabelDomains.max' must be at least 1, got 0",
		},
		{
			name:       "Case 3d: nodeLabelDomains with a negative max",
			simClients: true,
			params: map[string]interface{}{
				"refTaskId":        "step1",
				"nodeLabelDomains": map[string]interface{}{"key": rackLabel, "max": -1},
			},
			err: "CheckPod/check: parameter 'nodeLabelDomains.max' must be at least 1, got -1",
		},
		{
			name:       "Case 4: missing task reference",
			simClients: true,
			params: map[string]interface{}{
				"refTaskId":  "step1",
				"status":     "Running",
				"nodeLabels": map[string]string{"l1": "v1", "l2": "v2"},
				"timeout":    "1m",
			},
			err: "CheckPod/check: unreferenced task ID step1",
		},
		{
			name:       "Case 5: valid parameters",
			simClients: true,
			params: map[string]interface{}{
				"refTaskId":  "step1",
				"status":     "Running",
				"nodeLabels": map[string]string{"l1": "v1", "l2": "v2"},
				"timeout":    "1m",
			},
			refTaskId: "step1",
			task: &CheckPodTask{
				BaseTask: BaseTask{
					taskType: TaskCheckPod,
					taskID:   taskID,
				},
				checkPodTaskParams: checkPodTaskParams{
					RefTaskID:  "step1",
					Status:     "Running",
					NodeLabels: map[string]string{"l1": "v1", "l2": "v2"},
					Timeout:    time.Minute,
				},
				client: testK8sClient,
			},
		},
		{
			name:       "Case 6: nodeLabelDomains as the only check",
			simClients: true,
			params: map[string]interface{}{
				"refTaskId":        "step1",
				"nodeLabelDomains": map[string]interface{}{"key": rackLabel, "max": 1},
			},
			refTaskId: "step1",
			task: &CheckPodTask{
				BaseTask: BaseTask{
					taskType: TaskCheckPod,
					taskID:   taskID,
				},
				checkPodTaskParams: checkPodTaskParams{
					RefTaskID:        "step1",
					NodeLabelDomains: &nodeLabelDomains{Key: rackLabel, Max: 1},
				},
				client: testK8sClient,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(nil, nil, tc.simClients)
			require.NoError(t, err)
			if len(tc.refTaskId) != 0 {
				eng.objInfoMap[tc.refTaskId] = nil
			}
			task, err := eng.GetTask(&config.Task{
				ID:     taskID,
				Type:   TaskCheckPod,
				Params: tc.params,
			})
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
				require.Nil(t, tc.task)
			} else {
				tc.task.accessor = eng
				require.NoError(t, err)
				require.NotNil(t, tc.task)
				require.Equal(t, tc.task, task)
			}
		})
	}
}

const rackLabel = "cloud.provider.com/topology-rack"

// domainNodes are the nodes of the nodeLabelDomains tests: n1 and n2 share rack1,
// n3 is in rack2, n4 in rack5, and n5 has no rack label at all.
func domainNodes() []runtime.Object {
	node := func(name string, labels map[string]string) *v1.Node {
		return &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	}
	return []runtime.Object{
		node("n1", map[string]string{rackLabel: "rack1"}),
		node("n2", map[string]string{rackLabel: "rack1"}),
		node("n3", map[string]string{rackLabel: "rack2"}),
		node("n4", map[string]string{rackLabel: "rack5"}),
		node("n5", map[string]string{"cloud.provider.com/topology-block": "block1"}),
	}
}

func domainPod(name, node string, phase v1.PodPhase) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       v1.PodSpec{NodeName: node},
		Status:     v1.PodStatus{Phase: phase},
	}
}

// newDomainCheck builds a CheckPod task through the engine, as a workflow would,
// then points it at a fake cluster holding the domain nodes and the given pods.
func newDomainCheck(t *testing.T, params map[string]interface{}, pods []*v1.Pod) (*CheckPodTask, *fake.Clientset) {
	t.Helper()
	eng, err := New(nil, nil, true)
	require.NoError(t, err)
	eng.objInfoMap["job"] = &ObjInfo{
		Namespace: "default",
		PodCount:  len(pods),
		PodRegexp: []string{`^job-[0-9]+$`},
	}

	cfg := map[string]interface{}{"refTaskId": "job"}
	for key, val := range params {
		cfg[key] = val
	}
	runnable, err := eng.GetTask(&config.Task{ID: "check", Type: TaskCheckPod, Params: cfg})
	require.NoError(t, err)
	task, ok := runnable.(*CheckPodTask)
	require.True(t, ok)

	objs := domainNodes()
	for _, pod := range pods {
		objs = append(objs, pod)
	}
	client := fake.NewSimpleClientset(objs...)
	task.client = client
	return task, client
}

func TestCheckPodNodeLabelDomains(t *testing.T) {
	maxRacks := func(max int) map[string]interface{} {
		return map[string]interface{}{"key": rackLabel, "max": max}
	}
	testCases := []struct {
		name   string
		params map[string]interface{}
		pods   []*v1.Pod
		// errCheck is the expected error without a timeout (one pass),
		// errWatch with a timeout (watch mode); empty means the check passes.
		errCheck string
		errWatch string
	}{
		{
			name:   "4 pods on exactly max=2 racks pass",
			params: map[string]interface{}{"status": "Running", "nodeLabelDomains": maxRacks(2)},
			pods: []*v1.Pod{
				domainPod("job-0", "n1", v1.PodRunning),
				domainPod("job-1", "n2", v1.PodRunning),
				domainPod("job-2", "n3", v1.PodRunning),
				domainPod("job-3", "n3", v1.PodRunning),
			},
		},
		{
			name:   "2 pods on 2 nodes of one rack pass max=1",
			params: map[string]interface{}{"status": "Running", "nodeLabelDomains": maxRacks(1)},
			pods: []*v1.Pod{
				domainPod("job-0", "n1", v1.PodRunning),
				domainPod("job-1", "n2", v1.PodRunning),
			},
		},
		{
			name:   "3 racks fail max=2 with the values sorted",
			params: map[string]interface{}{"status": "Running", "nodeLabelDomains": maxRacks(2)},
			pods: []*v1.Pod{
				domainPod("job-0", "n4", v1.PodRunning),
				domainPod("job-1", "n3", v1.PodRunning),
				domainPod("job-2", "n1", v1.PodRunning),
				domainPod("job-3", "n2", v1.PodRunning),
			},
			errCheck: "CheckPod/check: pods on 3 distinct values of cloud.provider.com/topology-rack, at most 2 allowed: [rack1 rack2 rack5]",
			errWatch: "CheckPod/check: pods on 3 distinct values of cloud.provider.com/topology-rack, at most 2 allowed: [rack1 rack2 rack5]",
		},
		{
			name:   "2 racks fail max=1",
			params: map[string]interface{}{"status": "Running", "nodeLabelDomains": maxRacks(1)},
			pods: []*v1.Pod{
				domainPod("job-0", "n3", v1.PodRunning),
				domainPod("job-1", "n1", v1.PodRunning),
			},
			errCheck: "CheckPod/check: pods on 2 distinct values of cloud.provider.com/topology-rack, at most 1 allowed: [rack1 rack2]",
			errWatch: "CheckPod/check: pods on 2 distinct values of cloud.provider.com/topology-rack, at most 1 allowed: [rack1 rack2]",
		},
		{
			// Ignoring n5 would pass (one rack) and counting it as a value would
			// fail on 2 values; the check names the node instead.
			name:   "a node without the label fails the check",
			params: map[string]interface{}{"status": "Running", "nodeLabelDomains": maxRacks(1)},
			pods: []*v1.Pod{
				domainPod("job-0", "n1", v1.PodRunning),
				domainPod("job-1", "n5", v1.PodRunning),
			},
			errCheck: "CheckPod/check: node 'n5' of pod 'job-1' has no label 'cloud.provider.com/topology-rack'",
			errWatch: "CheckPod/check: node 'n5' of pod 'job-1' has no label 'cloud.provider.com/topology-rack'",
		},
		{
			name:   "nodeLabelDomains alone accepts any phase",
			params: map[string]interface{}{"nodeLabelDomains": maxRacks(1)},
			pods: []*v1.Pod{
				domainPod("job-0", "n1", v1.PodRunning),
				domainPod("job-1", "n2", v1.PodSucceeded),
			},
		},
		{
			// A pod without a node has no domain: one pass fails on it, the
			// watch waits for it to get a node and times out here.
			name:   "a pod without a node is not counted",
			params: map[string]interface{}{"nodeLabelDomains": maxRacks(1)},
			pods: []*v1.Pod{
				domainPod("job-0", "n1", v1.PodRunning),
				domainPod("job-1", "", v1.PodPending),
			},
			errCheck: "CheckPod/check: pod 'job-1' has no node, so nodeLabelDomains cannot place it",
			errWatch: "context deadline exceeded",
		},
		{
			name:   "status still applies with nodeLabelDomains",
			params: map[string]interface{}{"status": "Running", "nodeLabelDomains": maxRacks(1)},
			pods: []*v1.Pod{
				domainPod("job-0", "n1", v1.PodRunning),
				domainPod("job-1", "n2", v1.PodPending),
			},
			errCheck: "CheckPod/check: pod job-1, status Pending, expected Running",
			errWatch: "context deadline exceeded",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name+"/no timeout", func(t *testing.T) {
			task, _ := newDomainCheck(t, tc.params, tc.pods)
			err := task.Exec(context.Background())
			if len(tc.errCheck) == 0 {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.errCheck)
			}
		})
		t.Run(tc.name+"/timeout", func(t *testing.T) {
			params := map[string]interface{}{"timeout": "1s"}
			for key, val := range tc.params {
				params[key] = val
			}
			task, _ := newDomainCheck(t, params, tc.pods)
			err := task.Exec(context.Background())
			if len(tc.errWatch) == 0 {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.errWatch)
			}
		})
	}
}

// TestCheckPodNodeLabelDomainsWatchJudgesAllPods starts the watch while one pod
// has no node, then places it in a second rack: the spread is judged once every
// pod is accounted for, over all of them, so the late pod turns the check red.
func TestCheckPodNodeLabelDomainsWatchJudgesAllPods(t *testing.T) {
	pods := []*v1.Pod{
		domainPod("job-0", "n1", v1.PodRunning),
		domainPod("job-1", "", v1.PodPending),
	}
	params := map[string]interface{}{
		"status":           "Running",
		"timeout":          "5s",
		"nodeLabelDomains": map[string]interface{}{"key": rackLabel, "max": 1},
	}
	task, client := newDomainCheck(t, params, pods)

	done := make(chan error, 1)
	go func() { done <- task.Exec(context.Background()) }()

	time.Sleep(200 * time.Millisecond)
	late := domainPod("job-1", "n3", v1.PodRunning)
	_, err := client.CoreV1().Pods("default").Update(context.Background(), late, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.EqualError(t, <-done, "CheckPod/check: pods on 2 distinct values of cloud.provider.com/topology-rack, at most 1 allowed: [rack1 rack2]")
}
