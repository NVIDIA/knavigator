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

func TestWorkflow(t *testing.T) {
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

func TestWorkflowFile(t *testing.T) {
	c, err := NewFromFile("../../resources/workflows/test-custom-resource.yml")
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestWorkflowPaths(t *testing.T) {
	testCases := []struct {
		name  string
		paths string
		err   string
		count int
	}{
		{
			name: "Case 1: Empty string",
			err:  "empty filepaths",
		},
		{
			name:  "Case 2: Wrong path",
			paths: "a/b/c",
			err:   "stat a/b/c: no such file or directory",
		},
		{
			name:  "Case 3: Valid input",
			paths: "../../resources/workflows/volcano,../../resources/workflows/kueue/{test-job.yaml,test-preemption.yaml}",
			count: 3,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewFromPaths(tc.paths)
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
				require.Nil(t, c)
			} else {
				require.NoError(t, err)
				require.NotNil(t, c)
				require.Equal(t, tc.count, len(c))
			}
		})
	}
}

func TestParsePaths(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		paths []string
		err   string
	}{
		{
			name:  "Case 1a: valid, multi string",
			input: "a/b/c, dd/ee/{f,g,e},/x/y/zz",
			paths: []string{"a/b/c", "dd/ee/f", "dd/ee/g", "dd/ee/e", "/x/y/zz"},
		},
		{
			name:  "Case 1b: valid, single string",
			input: "a/b/c",
			paths: []string{"a/b/c"},
		},
		{
			name:  "Case 1c: valid, multiple braces",
			input: "dd/{ee}/{f,g,e}/xx/{yy,zz}",
			paths: []string{"dd/ee/f/xx/yy", "dd/ee/f/xx/zz", "dd/ee/g/xx/yy", "dd/ee/g/xx/zz", "dd/ee/e/xx/yy", "dd/ee/e/xx/zz"},
		},
		{
			name:  "Case 2a: unbalanced braces",
			input: "a/b/c{{",
			err:   `unbalanced braces in "a/b/c{{"`,
		},
		{
			name:  "Case 2b: unbalanced braces",
			input: "a/b/c}",
			err:   `unbalanced braces in "a/b/c}"`,
		},
		{
			name:  "Case 2c: unbalanced braces",
			input: "a/b/c{",
			err:   `unbalanced braces in "a/b/c{"`,
		},
		{
			name:  "Case 3: single character",
			input: "a",
			paths: []string{"a"},
		},
		{
			name:  "Case 4: empty string",
			input: "",
			paths: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			paths, err := parsePaths(tc.input)
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.paths, paths)
			}
		})
	}
}
