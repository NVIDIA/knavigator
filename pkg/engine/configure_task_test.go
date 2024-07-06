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

func TestNewConfigureTask(t *testing.T) {
	taskID := "configure"
	testCases := []struct {
		name       string
		simClients bool
		params     map[string]interface{}
		err        string
		task       *ConfigureTask
	}{
		{
			name:       "Case 1: no k8s client",
			simClients: false,
			params:     nil,
			err:        "Configure/configure: Kubernetes client is not set",
		},
		{
			name:       "Case 2: No parameters map",
			simClients: true,
			params:     nil,
			err:        "Configure/configure: missing parameter 'timeout'",
		},
		{
			name:       "Case 3: Invalid timeout value",
			simClients: true,
			params: map[string]interface{}{
				"timeout": "BAD",
			},
			err: "Configure/configure: failed to parse parameters: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!str `BAD` into time.Duration",
		},
		{
			name:       "Case 4: Invalid nodes type",
			simClients: true,
			params: map[string]interface{}{
				"timeout": "1m",
				"nodes":   "BAD",
			},
			err: "Configure/configure: failed to parse parameters: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!str `BAD` into []engine.virtualNode",
		},
		{
			name:       "Case 5a: Invalid namespace op",
			simClients: true,
			params: map[string]interface{}{
				"timeout":    "1m",
				"namespaces": []interface{}{map[string]interface{}{"name": "ns", "op": "BAD"}},
			},
			err: "Configure/configure: invalid namespace operation BAD; supported: create, delete",
		},
		{
			name:       "Case 5b: Invalid configmap op",
			simClients: true,
			params: map[string]interface{}{
				"timeout":    "1m",
				"configmaps": []interface{}{map[string]interface{}{"name": "ns", "op": "BAD"}},
			},
			err: "Configure/configure: invalid configmap operation BAD; supported: create, delete",
		},
		{
			name:       "Case 6: Valid parameters with default",
			simClients: true,
			params:     map[string]interface{}{"timeout": "1m"},
			task: &ConfigureTask{
				BaseTask: BaseTask{
					taskType: TaskConfigure,
					taskID:   taskID,
				},
				configureTaskParams: configureTaskParams{
					Timeout: time.Duration(time.Minute),
				},
				client: testK8sClient,
			},
		},
		{
			name:       "Case 7: Valid parameters without default",
			simClients: true,
			params: map[string]interface{}{
				"timeout": "1m",
				"nodes": []interface{}{
					map[string]interface{}{"type": "dgxa100.40g", "count": 2},
					map[string]interface{}{"type": "cpu-tiny", "count": 4},
				},
				"namespaces": []interface{}{
					map[string]interface{}{"name": "ns1", "op": "create"},
					map[string]interface{}{"name": "ns2", "op": "delete"},
				},
				"configmaps": []interface{}{
					map[string]interface{}{
						"name":      "cm1",
						"namespace": "default",
						"op":        "create",
						"data":      map[string]string{"key": "value"},
					},
				},
			},
			task: &ConfigureTask{
				BaseTask: BaseTask{
					taskType: TaskConfigure,
					taskID:   taskID,
				},
				configureTaskParams: configureTaskParams{
					Timeout: time.Duration(time.Minute),
					Nodes: []virtualNode{
						{
							Type:  "dgxa100.40g",
							Count: 2,
						},
						{
							Type:  "cpu-tiny",
							Count: 4,
						},
					},
					Namespaces: []namespace{
						{
							Name: "ns1",
							Op:   OpCreate,
						},
						{
							Name: "ns2",
							Op:   OpDelete,
						},
					},
					ConfigMaps: []configmap{
						{
							Name:      "cm1",
							Namespace: "default",
							Op:        "create",
							Data:      map[string]string{"key": "value"},
						},
					},
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
				Type:   TaskConfigure,
				Params: tc.params,
			})
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
				require.Nil(t, tc.task)
			} else {
				require.NoError(t, err)
				require.NotNil(t, tc.task)
				require.Equal(t, tc.task, task)
			}
		})
	}
}

func TestNodes2JSON(t *testing.T) {
	testCases := []struct {
		name  string
		nodes []virtualNode
		expr  string
	}{
		{
			name:  "Case 1: single entry",
			nodes: []virtualNode{{Type: "dgxa100.40g", Count: 2}},
			expr:  `nodes=[{"type":"dgxa100.40g","count":2}]`,
		},
		{
			name:  "Case 2: multiple entries",
			nodes: []virtualNode{{Type: "dgxa100.80g", Count: 4}, {Type: "cpu-tiny", Count: 4}},
			expr:  `nodes=[{"type":"dgxa100.80g","count":4},{"type":"cpu-tiny","count":4}]`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := nodes2json(tc.nodes)
			require.NoError(t, err)
			require.Equal(t, tc.expr, out)
		})
	}
}
