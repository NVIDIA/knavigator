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

func TestNewSubmitObjTask(t *testing.T) {
	taskID := "submit"
	grv := map[string]interface{}{
		"group":    "example.com",
		"version":  "v1",
		"resource": "myobjects",
	}
	overrides := map[string]interface{}{
		"instance": "lnx2000",
		"command":  "sleep infinity",
		"image":    "ubuntu",
		"cpu":      "100m",
		"memory":   "512M",
		"gpu":      8,
		"teamName": "teamName",
		"orgName":  "orgName",
		"userName": "tester",
	}
	spec := map[string]interface{}{
		"runPolicy": map[string]interface{}{
			"coScheduling": nil,
		},
		"template": map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{
					"orgName":  "orgName",
					"teamName": "teamName",
					"userName": "tester",
				},
				"labels": map[string]interface{}{
					"obj-name": "test",
					"instance": "lnx2000",
				},
			},
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":    "test",
						"args":    []interface{}{"-c", "sleep infinity"},
						"command": []interface{}{"/bin/sh"},
						"image":   "ubuntu",
						"resources": map[string]interface{}{
							"limits": map[string]interface{}{
								"cpu":            "100m",
								"memory":         "512M",
								"nvidia.com/gpu": "8",
							},
							"requests": map[string]interface{}{
								"cpu":    "100m",
								"memory": "512M",
							},
						},
					},
				},
			},
		},
	}
	testCases := []struct {
		name       string
		params     map[string]interface{}
		simClients bool
		err        string
		task       *SubmitObjTask
		pods       []string
	}{
		{
			name:       "Case 1: no client",
			params:     nil,
			simClients: false,
			err:        "SubmitObj/submit: DynamicClient is not set",
		},
		{
			name: "Case 2a: parsing error",
			params: map[string]interface{}{
				"count":     false,
				"grv":       grv,
				"template":  "../../resources/templates/example.yml",
				"overrides": overrides,
			},
			simClients: true,
			err:        "SubmitObj/submit: failed to parse parameters: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!bool `false` into int",
		},
		{
			name: "Case 2b: negative count",
			params: map[string]interface{}{
				"count":     -3,
				"grv":       grv,
				"template":  "../../resources/templates/example.yml",
				"overrides": overrides,
			},
			simClients: true,
			err:        "SubmitObj/submit: 'count' must be a positive number",
		},
		{
			name: "Case 2c: no template",
			params: map[string]interface{}{
				"grv":       grv,
				"overrides": overrides,
			},
			simClients: true,
			err:        "SubmitObj/submit: 'template' must be a filepath",
		},
		{
			name: "Case 2d: bad template",
			params: map[string]interface{}{
				"grv":       grv,
				"template":  "/does/not/exist",
				"overrides": overrides,
			},
			simClients: true,
			err:        "SubmitObj/submit: failed to parse template /does/not/exist: open /does/not/exist: no such file or directory",
		},
		{
			name: "Case 2e: no name format",
			params: map[string]interface{}{
				"count":     3,
				"grv":       grv,
				"template":  "../../resources/templates/example.yml",
				"overrides": overrides,
			},
			simClients: true,
			err:        "SubmitObj/submit: must specify name format for multiple object submissions",
		},
		{
			name: "Case 2f: name format error",
			params: map[string]interface{}{
				"count":      3,
				"grv":        grv,
				"template":   "../../resources/templates/example.yml",
				"nameformat": "{{{.}}",
				"overrides":  overrides,
			},
			simClients: true,
			err:        "SubmitObj/submit: failed to generate object names: template: name:1: unexpected \"{\" in command",
		},
		{
			name: "Case 3: Valid parameters without pod name selector",
			params: map[string]interface{}{
				"count":      1,
				"grv":        grv,
				"template":   "../../resources/templates/example.yml",
				"nameformat": "job{{._ENUM_}}",
				"overrides":  overrides,
			},
			simClients: true,
			task: &SubmitObjTask{
				BaseTask: BaseTask{
					log:      testLogger,
					taskType: TaskSubmitObj,
					taskID:   taskID,
				},
				submitObjTaskParams: submitObjTaskParams{
					Count: 1,
					GRV: groupVersionResource{
						Group:    "example.com",
						Version:  "v1",
						Resource: "myobjects",
					},
					Template:   "../../resources/templates/example.yml",
					NameFormat: "job{{._ENUM_}}",
					Overrides:  overrides,
				},
				client: testDynamicClient,
				obj: []GenericObject{
					{
						typeMeta: typeMeta{
							APIVersion: "example.com/v1",
							Kind:       "MyObject",
						},
						Metadata: objectMeta{
							Name:      "job1",
							Namespace: "test",
						},
						Spec: spec,
					},
				},
			},
			pods: []string{},
		},
		{
			name: "Case 4: Valid parameters with pod name selector",
			params: map[string]interface{}{
				"count":      2,
				"grv":        grv,
				"template":   "../../resources/templates/example.yml",
				"nameformat": "job{{._ENUM_}}",
				"overrides":  overrides,
				"pods": map[string]interface{}{
					"list": map[string]interface{}{
						"patterns": []string{"pod{{._NAME_}}"},
					},
					"range": map[string]interface{}{
						"pattern": "{{._NAME_}}-{{._INDEX_}}",
						"ranges":  []string{"0-1"},
					},
				},
			},
			simClients: true,
			task: &SubmitObjTask{
				BaseTask: BaseTask{
					log:      testLogger,
					taskType: TaskSubmitObj,
					taskID:   taskID,
				},
				submitObjTaskParams: submitObjTaskParams{
					Count: 2,
					GRV: groupVersionResource{
						Group:    "example.com",
						Version:  "v1",
						Resource: "myobjects",
					},
					Template:   "../../resources/templates/example.yml",
					NameFormat: "job{{._ENUM_}}",
					Overrides:  overrides,
				},
				client: testDynamicClient,
				obj: []GenericObject{
					{
						typeMeta: typeMeta{
							APIVersion: "example.com/v1",
							Kind:       "MyObject",
						},
						Metadata: objectMeta{
							Name:      "job1",
							Namespace: "test",
						},
						Spec: spec,
					},
					{
						typeMeta: typeMeta{
							APIVersion: "example.com/v1",
							Kind:       "MyObject",
						},
						Metadata: objectMeta{
							Name:      "job2",
							Namespace: "test",
						},
						Spec: spec,
					},
				},
			},
			pods: []string{"podjob1", "job1-0", "job1-1", "podjob2", "job2-0", "job2-1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			utils.SetObjectID(0)

			eng, err := New(testLogger, nil, tc.simClients)
			require.NoError(t, err)

			runnable, err := eng.GetTask(&config.Task{
				ID:     taskID,
				Type:   TaskSubmitObj,
				Params: tc.params,
			})
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
				require.Nil(t, tc.task)
			} else {
				tc.task.setter = eng
				require.NoError(t, err)
				require.NotNil(t, tc.task)

				task := runnable.(*SubmitObjTask)
				delete(task.Overrides, "_NAME_")
				delete(task.Overrides, "_ENUM_")

				require.Equal(t, tc.pods, task.Pods.Names())
				task.Pods = utils.NameSelector{}

				require.Equal(t, tc.task, task)
			}
		})
	}
}
