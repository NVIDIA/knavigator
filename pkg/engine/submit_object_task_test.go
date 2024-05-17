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
	"text/template"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

func TestNewSubmitObjTask(t *testing.T) {
	taskID := "submit"
	params := map[string]interface{}{
		"replicas": 2,
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
				"replicas": 2,
			},
		},
	}
	testCases := []struct {
		name         string
		params       map[string]interface{}
		simClients   bool
		regObjParams *RegisterObjParams
		refTaskID    string
		err          string
		task         *SubmitObjTask
		objs         []GenericObject
		names        []string
		podCount     int
		podRegexp    []string
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
				"count":  false,
				"params": params,
			},
			simClients: true,
			err:        "SubmitObj/submit: failed to parse parameters: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!bool `false` into int",
		},
		{
			name: "Case 2b: missing refTaskId",
			params: map[string]interface{}{
				"params": params,
			},
			simClients: true,
			err:        "SubmitObj/submit: must specify refTaskId",
		},
		{
			name: "Case 2c: negative count",
			params: map[string]interface{}{
				"refTaskId": "register",
				"count":     -3,
				"params":    params,
			},
			simClients: true,
			err:        "SubmitObj/submit: 'count' must be a positive number",
		},
		{
			name: "Case 2d: negative count",
			params: map[string]interface{}{
				"refTaskId": "register",
				"count":     1,
				"params":    params,
			},
			simClients: true,
			regObjParams: &RegisterObjParams{
				Template:   "../../resources/templates/example.yml",
				NameFormat: "job{{._ENUM_}}",
			},
			err: "SubmitObj/submit: unreferenced task ID register",
		},
		{
			name: "Case 3: Valid parameters without pods",
			params: map[string]interface{}{
				"refTaskId": "register",
				"count":     1,
				"params":    params,
			},
			simClients: true,
			regObjParams: &RegisterObjParams{
				Template:   "../../resources/templates/example.yml",
				NameFormat: "job{{._ENUM_}}",
			},
			refTaskID: "register",
			task: &SubmitObjTask{
				BaseTask: BaseTask{
					log:      testLogger,
					taskType: TaskSubmitObj,
					taskID:   taskID,
				},
				submitObjTaskParams: submitObjTaskParams{
					RefTaskID: "register",
					Count:     1,
					Params:    params,
				},
				client: testDynamicClient,
			},
			objs: []GenericObject{
				{
					TypeMeta: TypeMeta{
						APIVersion: "example.com/v1",
						Kind:       "MyObject",
					},
					Metadata: objectMeta{
						Name:      "job1",
						Namespace: "default",
					},
					Spec: spec,
				},
			},
			names:     []string{"job1"},
			podRegexp: []string{},
		},
		{
			name: "Case 4: Valid parameters with pods",
			params: map[string]interface{}{
				"refTaskId": "register",
				"count":     2,
				"params":    params,
			},
			simClients: true,
			regObjParams: &RegisterObjParams{
				Template:      "../../resources/templates/example.yml",
				NameFormat:    "job{{._ENUM_}}",
				PodNameFormat: "{{._NAME_}}-test-[0-9]+",
				PodCount:      "{{.replicas}}",
			},
			refTaskID: "register",
			task: &SubmitObjTask{
				BaseTask: BaseTask{
					log:      testLogger,
					taskType: TaskSubmitObj,
					taskID:   taskID,
				},
				submitObjTaskParams: submitObjTaskParams{
					RefTaskID: "register",
					Count:     2,
					Params:    params,
				},
				client: testDynamicClient,
			},
			objs: []GenericObject{
				{
					TypeMeta: TypeMeta{
						APIVersion: "example.com/v1",
						Kind:       "MyObject",
					},
					Metadata: objectMeta{
						Name:      "job1",
						Namespace: "default",
					},
					Spec: spec,
				},
				{
					TypeMeta: TypeMeta{
						APIVersion: "example.com/v1",
						Kind:       "MyObject",
					},
					Metadata: objectMeta{
						Name:      "job2",
						Namespace: "default",
					},
					Spec: spec,
				},
			},
			names:     []string{"job1", "job2"},
			podCount:  4,
			podRegexp: []string{"job1-test-[0-9]+", "job2-test-[0-9]+"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			utils.SetObjectID(0)

			eng, err := New(testLogger, nil, tc.simClients)
			require.NoError(t, err)

			if len(tc.refTaskID) != 0 {
				eng.objTypeMap[tc.refTaskID] = tc.regObjParams
			}

			runnable, err := eng.GetTask(&config.Task{
				ID:     taskID,
				Type:   TaskSubmitObj,
				Params: tc.params,
			})
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
				require.Nil(t, tc.task)
			} else {
				tc.task.accessor = eng
				require.NoError(t, err)
				require.NotNil(t, tc.task)

				task := runnable.(*SubmitObjTask)
				delete(task.Params, "_NAME_")
				delete(task.Params, "_ENUM_")

				require.Equal(t, tc.task, task)

				tc.regObjParams.objTpl, err = template.ParseFiles(tc.regObjParams.Template)
				require.NoError(t, err)

				if len(tc.regObjParams.PodNameFormat) != 0 {
					tc.regObjParams.podNameTpl, err = template.New("podname").Parse(tc.regObjParams.PodNameFormat)
					require.NoError(t, err)
				}

				if len(tc.regObjParams.PodCount) != 0 {
					tc.regObjParams.podCountTpl, err = template.New("podcount").Parse(tc.regObjParams.PodCount)
					require.NoError(t, err)
				}

				objs, names, podCount, podRegexp, err := task.getGenericObjects(tc.regObjParams)
				require.NoError(t, err)
				require.Equal(t, tc.objs, objs)
				require.Equal(t, tc.names, names)
				require.Equal(t, tc.podCount, podCount)
				require.Equal(t, tc.podRegexp, podRegexp)
			}
		})
	}
}
