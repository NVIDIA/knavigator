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
)

func TestObjStateTaskValidate(t *testing.T) {
	testCases := []struct {
		name   string
		params map[string]interface{}
		state  StateParams
		err    string
	}{
		{
			name:   "Case 1: invalid input",
			params: map[string]interface{}{"state": 1},
			err:    "Test/step: failed to parse parameters: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!int `1` into map[string]interface {}",
		},
		{
			name:   "Case 2: missing refTaskId",
			params: map[string]interface{}{},
			err:    "Test/step: missing parameter 'refTaskId'",
		},
		{
			name:   "Case 3: missing state",
			params: map[string]interface{}{"refTaskId": 1},
			err:    "Test/step: missing parameter 'state'",
		},
		{
			name: "Case 4: valid input with default",
			params: map[string]interface{}{
				"refTaskId": 1,
				"state":     map[string]interface{}{"a": 1},
			},
			state: StateParams{
				RefTaskID: "1",
				State:     map[string]interface{}{"a": 1},
				Timeout:   0,
			},
		},
		{
			name: "Case 5: valid input without default",
			params: map[string]interface{}{
				"refTaskId": 1,
				"state":     map[string]interface{}{"a": "1"},
				"timeout":   "1m",
			},
			state: StateParams{
				RefTaskID: "1",
				State:     map[string]interface{}{"a": "1"},
				Timeout:   time.Minute,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			task := &ObjStateTask{
				BaseTask: BaseTask{
					taskType: "Test",
					taskID:   "step",
				},
			}
			err := task.validate(tc.params)
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.state, task.StateParams)
			}
		})
	}
}
