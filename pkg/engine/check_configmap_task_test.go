package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/config"
)

func TestCheckConfigmapTask(t *testing.T) {
	taskID := "check"
	testCases := []struct {
		name       string
		simClients bool
		params     map[string]interface{}
		err        string
		task       *CheckConfigmapTask
	}{
		{
			name:   "Case 1: no k8s client",
			params: nil,
			err:    "CheckConfigmap/check: Kubernetes client is not set",
		},
		{
			name:       "Case 2: no params",
			simClients: true,
			params:     nil,
			err:        "CheckConfigmap/check: must specify name and namespace",
		},
		{
			name:       "Case 3: bad params",
			simClients: true,
			params: map[string]interface{}{
				"name": "test", "namespace": "default", "data": 1,
			},
			err: "CheckConfigmap/check: failed to parse parameters: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!int `1` into map[string]string",
		},
		{
			name:       "Case 4: invalid op",
			simClients: true,
			params: map[string]interface{}{
				"name":      "test",
				"namespace": "default",
				"data":      map[string]string{"key": "val"},
				"op":        "BAD",
			},
			err: "CheckConfigmap/check: invalid configmap operation BAD; supported: equal, subset",
		},
		{
			name:       "Case 5: valid input",
			simClients: true,
			params: map[string]interface{}{
				"name":      "test",
				"namespace": "default",
				"data":      map[string]string{"key": "val"},
				"op":        "equal",
			},
			task: &CheckConfigmapTask{
				BaseTask: BaseTask{taskType: TaskCheckConfigmap, taskID: taskID},
				checkConfigmapTaskParams: checkConfigmapTaskParams{
					Name:      "test",
					Namespace: "default",
					Data:      map[string]string{"key": "val"},
					Op:        OpCmpEqual,
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
				Type:   TaskCheckConfigmap,
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

func TestCompareConfigMaps(t *testing.T) {
	taskID := "check"
	testCases := []struct {
		name   string
		task   *CheckConfigmapTask
		actual map[string]string
		err    string
	}{
		{
			name: "Case 1: expected less than present",
			task: &CheckConfigmapTask{
				BaseTask: BaseTask{taskType: TaskCheckConfigmap, taskID: taskID},
				checkConfigmapTaskParams: checkConfigmapTaskParams{
					Name:      "test",
					Namespace: "default",
					Data:      map[string]string{"a": "b"},
					Op:        OpCmpEqual,
				},
			},
			actual: map[string]string{"a": "b", "c": "d"},
			err:    "CheckConfigmap/check: configmap default/test has 2 items; expected 1",
		},
		{
			name: "Case 2: expected more than present",
			task: &CheckConfigmapTask{
				BaseTask: BaseTask{taskType: TaskCheckConfigmap, taskID: taskID},
				checkConfigmapTaskParams: checkConfigmapTaskParams{
					Name:      "test",
					Namespace: "default",
					Data:      map[string]string{"a": "b", "c": "d"},
					Op:        OpCmpSubset,
				},
			},
			actual: map[string]string{"a": "b"},
			err:    "CheckConfigmap/check: configmap default/test has 1 items; expected 2",
		},
		{
			name: "Case 3: key mismatch",
			task: &CheckConfigmapTask{
				BaseTask: BaseTask{taskType: TaskCheckConfigmap, taskID: taskID},
				checkConfigmapTaskParams: checkConfigmapTaskParams{
					Name:      "test",
					Namespace: "default",
					Data:      map[string]string{"a": "b", "c": "d"},
					Op:        OpCmpEqual,
				},
			},
			actual: map[string]string{"a": "b", "e": "f"},
			err:    "CheckConfigmap/check: configmap default/test does not have key c",
		},
		{
			name: "Case 4: valid OpCmpEqual case",
			task: &CheckConfigmapTask{
				BaseTask: BaseTask{taskType: TaskCheckConfigmap, taskID: taskID},
				checkConfigmapTaskParams: checkConfigmapTaskParams{
					Name:      "test",
					Namespace: "default",
					Data:      map[string]string{"a": "b", "c": "d"},
					Op:        OpCmpEqual,
				},
			},
			actual: map[string]string{"a": "b", "c": "d"},
		},
		{
			name: "Case 5: valid OpCmpSubset case",
			task: &CheckConfigmapTask{
				BaseTask: BaseTask{taskType: TaskCheckConfigmap, taskID: taskID},
				checkConfigmapTaskParams: checkConfigmapTaskParams{
					Name:      "test",
					Namespace: "default",
					Data:      map[string]string{"a": "b", "c": "d"},
					Op:        OpCmpSubset,
				},
			},
			actual: map[string]string{"a": "b", "c": "d", "e": "f"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.task.compareConfigMaps(tc.actual)
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
