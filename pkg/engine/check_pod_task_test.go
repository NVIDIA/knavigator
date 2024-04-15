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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
			err: "CheckPod/check: missing parameters 'status' and/or 'nodeLabels'",
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
					log:      testLogger,
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(testLogger, nil, tc.simClients)
			require.NoError(t, err)
			if len(tc.refTaskId) != 0 {
				eng.objMap[tc.refTaskId] = nil
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
				tc.task.getter = eng
				require.NoError(t, err)
				require.NotNil(t, tc.task)
				require.Equal(t, tc.task, task)
			}
		})
	}
}
