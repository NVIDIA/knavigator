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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

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
			eng, err := New(nil, nil, tc.simClients)
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

// A fake API server that records patched paths and bodies.
type patchRecorder struct {
	mu      sync.Mutex
	patches map[string][]byte // path -> body
}

func (r *patchRecorder) handler(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodPatch {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.patches[req.URL.Path] = body
		r.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"apiVersion":"g/v","kind":"Job","metadata":{"name":"job0","namespace":"default"}}`)
}

func (r *patchRecorder) bodyFor(t *testing.T, suffix string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	base := "/apis/g/v/namespaces/default/jobs/job0"
	body, ok := r.patches[base+suffix]
	require.True(t, ok, "no patch to %s", base+suffix)
	return string(body)
}

// UpdateObj must send the status part of the state to the /status subresource,
// not the root patch: a status-only state has no root patch at all.
func TestUpdateObjTaskExecPatchesStatusSubresource(t *testing.T) {
	rec := &patchRecorder{patches: map[string][]byte{}}
	ts := httptest.NewServer(http.HandlerFunc(rec.handler))
	defer ts.Close()

	cfg := &rest.Config{Host: ts.URL, ContentConfig: rest.ContentConfig{GroupVersion: &schema.GroupVersion{Group: "g", Version: "v"}}}
	client, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)

	eng, err := New(nil, nil, true)
	require.NoError(t, err)
	eng.objInfoMap["submit"] = NewObjInfo([]string{"job0"}, "default",
		[]schema.GroupVersionResource{{Group: "g", Version: "v", Resource: "jobs"}}, 0)

	// A state with spec and status patches both subresources.
	task := &UpdateObjTask{ObjStateTask: ObjStateTask{
		BaseTask: BaseTask{taskType: TaskUpdateObj, taskID: "update"},
		StateParams: StateParams{RefTaskID: "submit", State: map[string]interface{}{
			"spec":   map[string]interface{}{"suspend": true},
			"status": map[string]interface{}{"active": 3},
		}},
		client:   client,
		accessor: eng,
	}}
	require.NoError(t, task.Exec(context.Background()))
	require.JSONEq(t, `{"spec":{"suspend":true}}`, rec.bodyFor(t, ""))
	require.JSONEq(t, `{"status":{"active":3}}`, rec.bodyFor(t, "/status"))

	// A state with only status reaches the subresource with a well-formed patch.
	rec.mu.Lock()
	rec.patches = map[string][]byte{}
	rec.mu.Unlock()
	task.State = map[string]interface{}{"status": map[string]interface{}{"active": 2}}
	require.NoError(t, task.Exec(context.Background()))
	require.JSONEq(t, `{"status":{"active":2}}`, rec.bodyFor(t, "/status"))
}
