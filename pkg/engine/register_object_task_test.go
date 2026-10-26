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

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"

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
				gvk: []schema.GroupVersionKind{
					{
						Group:   "example.com",
						Version: "v1",
						Kind:    "MyObject",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(nil, nil, tc.simClients)
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
				require.NoError(t, err)
				require.NotNil(t, tc.task)
				tc.task.accessor = eng

				task := runnable.(*RegisterObjTask)
				task.objTpl, task.podNameTpl, task.podCountTpl = nil, nil, nil
				require.Equal(t, tc.task, task)
			}
		})
	}
}

func TestRegisterObjExecMapsBuiltInKinds(t *testing.T) {
	// The API server leaves Group and Version empty on built-in resources.
	client := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{Resources: []*metav1.APIResourceList{
		{GroupVersion: "batch/v1", APIResources: []metav1.APIResource{{Name: "jobs", Kind: "Job", Namespaced: true}}},
		{GroupVersion: "scheduling.k8s.io/v1beta1", APIResources: []metav1.APIResource{
			{Name: "workloads", Kind: "Workload", Namespaced: true},
		}},
		{GroupVersion: "scheduling.k8s.io/v1alpha3", APIResources: []metav1.APIResource{
			{Name: "workloads", Kind: "Workload", Namespaced: true},
			{Name: "compositepodgroups", Kind: "CompositePodGroup", Namespaced: true},
		}},
	}}}
	eng, err := New(nil, nil, true)
	require.NoError(t, err)

	task := &RegisterObjTask{
		BaseTask: BaseTask{taskType: TaskRegisterObj, taskID: "register"},
		client:   client,
		accessor: eng,
		gvk: []schema.GroupVersionKind{
			{Group: "batch", Version: "v1", Kind: "Job"},
			{Group: "scheduling.k8s.io", Version: "v1beta1", Kind: "Workload"},
			{Group: "scheduling.k8s.io", Version: "v1alpha3", Kind: "CompositePodGroup"},
		},
	}
	require.NoError(t, task.Exec(context.Background()))
	require.Equal(t, []schema.GroupVersionResource{
		{Group: "batch", Version: "v1", Resource: "jobs"},
		{Group: "scheduling.k8s.io", Version: "v1beta1", Resource: "workloads"},
		{Group: "scheduling.k8s.io", Version: "v1alpha3", Resource: "compositepodgroups"},
	}, task.gvr)

	task.gvk = []schema.GroupVersionKind{{Group: "scheduling.k8s.io", Version: "v1alpha2", Kind: "Workload"}}
	require.Error(t, task.Exec(context.Background()))
}
