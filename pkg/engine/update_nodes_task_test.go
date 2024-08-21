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

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/config"
)

func TestUpdateNodesTask(t *testing.T) {
	taskID := "update"
	testCases := []struct {
		name       string
		params     map[string]interface{}
		simClients bool
		err        string
		task       *UpdateNodesTask
	}{
		{
			name:   "Case 1: no k8s client",
			params: nil,
			err:    "kubernetes clientset not set",
		},
		{
			name:       "Case 2: no parameters map",
			simClients: true,
			err:        "missing node selectors in UpdateNodes task update",
		},
		{
			name: "Case 3: invalid params",
			params: map[string]interface{}{
				"selectors": false,
			},
			simClients: true,
			err:        "failed to parse parameters in UpdateNodes task update: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!bool `false` into []map[string]string",
		},
		{
			name: "Case 4: missing state parameters",
			params: map[string]interface{}{
				"selectors": []map[string]string{{"key1": "val1"}},
			},
			simClients: true,
			err:        "missing state parameters in UpdateNodes task update",
		},
		{
			name: "Case 5: valid input",
			params: map[string]interface{}{
				"selectors": []map[string]string{{"key1": "val1"}, {"key2": "val2", "key3": "val3"}},
				"state": map[string]interface{}{
					"spec": map[string]interface{}{"unschedulable": true},
				},
			},
			simClients: true,
			task: &UpdateNodesTask{
				BaseTask: BaseTask{
					taskType: TaskUpdateNodes,
					taskID:   taskID,
				},
				nodeStateParams: nodeStateParams{
					StateParams: StateParams{
						State: map[string]interface{}{
							"spec": map[string]interface{}{"unschedulable": true},
						},
					},
					Selectors: []map[string]string{{"key1": "val1"}, {"key2": "val2", "key3": "val3"}},
				},
				client: testK8sClient,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(nil, nil, tc.simClients)
			require.NoError(t, err)
			task, err := eng.GetTask(&config.Task{
				ID:     taskID,
				Type:   TaskUpdateNodes,
				Params: tc.params,
			})
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
				require.Nil(t, tc.task)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.task, task)
			}
		})
	}
}

func TestIsMapSubset(t *testing.T) {
	testCases := []struct {
		name   string
		set    map[string]string
		subset map[string]string
		res    bool
	}{
		{
			name: "Case 1: empty subset",
			set:  map[string]string{"key1": "val1"},
			res:  true,
		},
		{
			name:   "Case 2: empty set",
			subset: map[string]string{"key1": "val1"},
			res:    false,
		},
		{
			name:   "Case 3: equal sets",
			set:    map[string]string{"key1": "val1", "key2": "val2", "key3": "val3"},
			subset: map[string]string{"key1": "val1", "key2": "val2", "key3": "val3"},
			res:    true,
		},
		{
			name:   "Case 4: valid subset",
			set:    map[string]string{"key1": "val1", "key2": "val2", "key3": "val3"},
			subset: map[string]string{"key1": "val1"},
			res:    true,
		},
		{
			name:   "Case 5: invalid subset",
			set:    map[string]string{"key1": "val1", "key2": "val2"},
			subset: map[string]string{"key3": "val3"},
			res:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.res, isMapSubset(tc.set, tc.subset))
		})
	}
}
