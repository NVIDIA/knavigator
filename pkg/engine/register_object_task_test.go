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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/NVIDIA/knavigator/pkg/config"
)

func TestNewRegisterObjTask(t *testing.T) {
	taskID := "register"
	testCases := []struct {
		name       string
		params     map[string]interface{}
		simClients bool
		err        string
		task       *RegisterObjTask
		pods       []string
	}{
		{
			name:       "Case 1: no client",
			params:     nil,
			simClients: false,
			err:        "RegisterObj/register: DiscoveryClient is not set",
		},
		{
			name:       "Case 2: missing template",
			params:     map[string]interface{}{},
			simClients: true,
			err:        "RegisterObj/register: must specify template",
		},
		{
			name: "Case 3: bad template path",
			params: map[string]interface{}{
				"template": "/does/not/exist",
			},
			simClients: true,
			err:        "RegisterObj/register: failed to read /does/not/exist: open /does/not/exist: no such file or directory",
		},
		{
			name: "Case 5: bad podNameFormat",
			params: map[string]interface{}{
				"template":      "../../resources/templates/example.yml",
				"nameFormat":    "test",
				"podNameFormat": "test{{",
			},
			simClients: true,
			err:        "RegisterObj/register: failed to parse podname template: template: podname:1: unclosed action",
		},
		{
			name: "Case 6: bad podCount",
			params: map[string]interface{}{
				"template":      "../../resources/templates/example.yml",
				"nameFormat":    "test",
				"podNameFormat": "test{{._NAME_}}",
				"podCount":      "test{{",
			},
			simClients: true,
			err:        "RegisterObj/register: failed to parse podcount template: template: podcount:1: unclosed action",
		},
		{
			name: "Case 7: missing podCount",
			params: map[string]interface{}{
				"template":      "../../resources/templates/example.yml",
				"nameFormat":    "test",
				"podNameFormat": "test{{._NAME_}}",
			},
			simClients: true,
			err:        "RegisterObj/register: must define podCount with podNameFormat",
		},
		{
			name: "Case 8: missing podNameFormat",
			params: map[string]interface{}{
				"template":   "../../resources/templates/example.yml",
				"nameFormat": "test",
				"podCount":   "2",
			},
			simClients: true,
			err:        "RegisterObj/register: must define podNameFormat with podCount",
		},
		{
			name: "Case 9: valid input",
			params: map[string]interface{}{
				"template":      "../../resources/templates/example.yml",
				"nameFormat":    "test",
				"podNameFormat": "test{{._NAME_}}",
				"podCount":      "2",
			},
			simClients: true,
			task: &RegisterObjTask{
				BaseTask: BaseTask{
					log:      testLogger,
					taskType: TaskRegisterObj,
					taskID:   taskID,
				},
				RegisterObjParams: RegisterObjParams{
					Template:      "../../resources/templates/example.yml",
					NameFormat:    "test",
					PodNameFormat: "test{{._NAME_}}",
					PodCount:      "2",
				},
				client: testDiscoveryClient,
				gvk: schema.GroupVersionKind{
					Group:   "example.com",
					Version: "v1",
					Kind:    "MyObject",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(testLogger, nil, nil, tc.simClients)
			require.NoError(t, err)

			runnable, err := eng.GetTask(&config.Task{
				ID:     taskID,
				Type:   TaskRegisterObj,
				Params: tc.params,
			})
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
				require.Nil(t, tc.task)
			} else {
				tc.task.accessor = eng
				require.NoError(t, err)
				require.NotNil(t, tc.task)

				task := runnable.(*RegisterObjTask)
				task.objTpl, task.podNameTpl, task.podCountTpl = nil, nil, nil
				require.Equal(t, tc.task, task)
			}
		})
	}
}
