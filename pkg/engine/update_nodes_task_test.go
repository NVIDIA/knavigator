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
	"github.com/NVIDIA/knavigator/pkg/utils"
)

func TestUpdateNodesTask(t *testing.T) {
	taskID := "update"
	testCases := []struct {
		name       string
		params     map[string]interface{}
		simClients bool
		err        string
		task       *UpdateNodesTask
		patch      *utils.PatchData
	}{
		{
			name:   "Case 1: no k8s client",
			params: nil,
			err:    "kubernetes clientset not set",
		},
		{
			name:       "Case 2: no parameters map",
			simClients: true,
			err:        "missing node selector in UpdateNodes task update",
		},
		{
			name: "Case 3: invalid params",
			params: map[string]interface{}{
				"selector": false,
			},
			simClients: true,
			err:        "failed to parse parameters in UpdateNodes task update: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!bool `false` into utils.NameSelector",
		},
		{
			name: "Case 4: no range pattern",
			params: map[string]interface{}{
				"selector": map[string]interface{}{
					"list": map[string]interface{}{
						"patterns": []interface{}{"node1", "node2"},
					},
					"range": map[string]interface{}{},
				},
			},
			simClients: true,
			err:        "failed to parse parameters in UpdateNodes task update: missing pattern in name range",
		},
		{
			name: "Case 5: missing state parameters",
			params: map[string]interface{}{
				"selector": map[string]interface{}{
					"list": map[string]interface{}{
						"patterns": []interface{}{"node1", "node2"},
					},
					"range": map[string]interface{}{
						"pattern": "node{{._INDEX_}}",
						"ranges":  []string{"2-4"},
					},
				},
			},
			simClients: true,
			err:        "missing state parameters in UpdateNodes task update",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(nil, nil, tc.simClients)
			require.NoError(t, err)
			_, err = eng.GetTask(&config.Task{
				ID:     taskID,
				Type:   TaskUpdateNodes,
				Params: tc.params,
			})
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
				require.Nil(t, tc.task)
			}
		})
	}
}
