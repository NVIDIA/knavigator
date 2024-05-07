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

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskConfig(t *testing.T) {
	testCases := []struct {
		name   string
		config string
		err    string
	}{
		{
			name:   "Case 1: missing tasks",
			config: "name: test001",
			err:    "test test001 has no tasks",
		},
		{
			name: "Case 2: missing task type",
			config: `
name: test
tasks:
- id: task1
  type: Task1
  params:
    type: task1
- id: task2
  params:
    type: task2`,
			err: "missing task type for tasks[1]",
		},
		{
			name: "Case 3: missing task ID",
			config: `
name: test
tasks:
- type: Task1
  description: task 1
  params:
    type: task1
- id: task2
  type: Task2
  description: task 2
  params:
    type: task2`,
			err: "missing task ID for tasks[0]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New([]byte(tc.config))
			if len(tc.err) != 0 {
				require.Error(t, err)
				require.Equal(t, err.Error(), tc.err)
				require.Nil(t, c)
			} else {
				require.Nil(t, err)
				require.NotNil(t, c)
			}
		})
	}
}

func TestConfigFile(t *testing.T) {
	c, err := NewFromFile("../../resources/tests/test-custom-resource.yml")
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestConfigPaths(t *testing.T) {
	testCases := []struct {
		name  string
		paths string
		err   string
		count int
	}{
		{
			name: "Case 1: Empty string",
			err:  "stat : no such file or directory",
		},
		{
			name:  "Case 2: Wrong path",
			paths: "a/b/c",
			err:   "stat a/b/c: no such file or directory",
		},
		{
			name:  "Case 3: Valid input",
			paths: "../../resources/tests/volcano,../../resources/tests/test-custom-resource.yml",
			count: 2,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewFromPaths(tc.paths)
			if len(tc.err) != 0 {
				require.Error(t, err)
				require.Equal(t, err.Error(), tc.err)
				require.Nil(t, c)
			} else {
				require.Nil(t, err)
				require.NotNil(t, c)
				require.Equal(t, tc.count, len(c))
			}
		})
	}
}
