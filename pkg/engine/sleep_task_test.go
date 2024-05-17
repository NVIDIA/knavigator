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
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/config"
)

func TestSleepParams(t *testing.T) {
	taskID := "sleep"
	testCases := []struct {
		name   string
		params map[string]interface{}
		err    string
		task   *SleepTask
	}{
		{
			name:   "Case 1: No parameters map",
			params: nil,
			err:    "Sleep/sleep: 'timeout' parameter have value",
		},
		{
			name: "Case 2: Invalid timeout value",
			params: map[string]interface{}{
				"timeout": "BAD",
			},
			err: "Sleep/sleep: failed to parse parameters: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!str `BAD` into time.Duration",
		},
		{
			name: "Case 3: Valid parameters",
			params: map[string]interface{}{
				"timeout": "1m",
			},
			task: &SleepTask{
				BaseTask: BaseTask{
					log:      testLogger,
					taskType: TaskSleep,
					taskID:   taskID,
				},
				sleepTaskParams: sleepTaskParams{
					Timeout: time.Minute,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(testLogger, nil, nil, false)
			require.NoError(t, err)

			task, err := eng.GetTask(&config.Task{
				ID:     taskID,
				Type:   TaskSleep,
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

func TestSleepExec(t *testing.T) {
	testCases := []struct {
		name    string
		timeout string
	}{
		{
			name:    "Case 1: undertime",
			timeout: "2s",
		},
		{
			name:    "Case 2: overtime",
			timeout: "4s",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			task, err := newSleepTask(testLogger, &config.Task{
				ID:     "sleep",
				Type:   TaskSleep,
				Params: map[string]interface{}{"timeout": tc.timeout},
			})
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			done := make(chan error)

			go func() {
				done <- task.Exec(ctx)
			}()

			select {
			case err = <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				require.Error(t, ctx.Err())
			}
		})
	}
}
