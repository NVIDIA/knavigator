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

func TestNewUpdateObjTask(t *testing.T) {
	taskID := "update"
	testCases := []struct {
		name       string
		params     map[string]interface{}
		simClients bool
		refTaskId  string
		err        string
		task       *UpdateObjTask
	}{
		{
			name:       "Case 1: no client",
			simClients: false,
			err:        "UpdateObj/update: DynamicClient is not set",
		},
		{
			name: "Case 2: failed validation",
			params: map[string]interface{}{
				"Timeout": "5s",
			},
			simClients: true,
			err:        "UpdateObj/update: missing parameter 'refTaskId'",
		},
		{
			name: "Case 3: missing task reference",
			params: map[string]interface{}{
				"refTaskId": 1,
				"state":     map[string]interface{}{"a": "b"},
				"Timeout":   "5s",
			},
			simClients: true,
			err:        "UpdateObj/update: unreferenced task ID 1",
		},
		{
			name: "Case 4: valid input",
			params: map[string]interface{}{
				"refTaskId": 1,
				"state":     map[string]interface{}{"a": "b"},
				"timeout":   "5s",
			},
			simClients: true,
			refTaskId:  "1",
			task: &UpdateObjTask{
				ObjStateTask: ObjStateTask{
					BaseTask: BaseTask{
						log:      testLogger,
						taskType: TaskUpdateObj,
						taskID:   taskID,
					},
					StateParams: StateParams{
						RefTaskID: "1",
						State:     map[string]interface{}{"a": "b"},
						Timeout:   5 * time.Second,
					},
					client: testDynamicClient,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(testLogger, nil, nil, tc.simClients)
			require.NoError(t, err)
			if len(tc.refTaskId) != 0 {
				eng.objInfoMap[tc.refTaskId] = nil
			}

			task, err := eng.GetTask(&config.Task{
				ID:     taskID,
				Type:   TaskUpdateObj,
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
