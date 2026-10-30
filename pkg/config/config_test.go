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
	"os"
	"path/filepath"
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
	c, err := NewFromFile("../../resources/workflows/test-custom-resource.yml", nil)
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
			c, err := NewFromPaths(tc.paths, nil)
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

func TestWorkflowVars(t *testing.T) {
	workflow := `name: vars
tasks:
  - id: fleet
    type: Configure
    params:
      count: ${nodes:-64}
      sizes: [${size}]
      empty: '${blank:-}'
      shell: echo $${HOME}
      label: "${label:-x}"
`
	testCases := []struct {
		name   string
		vars   Vars
		params map[string]interface{}
		err    string
	}{
		{
			name:   "Case 1: defaults and a set value keep YAML types",
			vars:   Vars{"size": "48"},
			params: map[string]interface{}{"count": 64, "sizes": []interface{}{48}, "empty": "", "shell": "echo ${HOME}", "label": "x"},
		},
		{
			name:   "Case 2: a value overrides the default",
			vars:   Vars{"nodes": "2048", "size": "2048"},
			params: map[string]interface{}{"count": 2048, "sizes": []interface{}{2048}, "empty": "", "shell": "echo ${HOME}", "label": "x"},
		},
		{
			name:   "Case 2b: empty values and a value with YAML syntax",
			vars:   Vars{"size": "", "label": "a: b #c"},
			params: map[string]interface{}{"count": 64, "sizes": []interface{}{}, "empty": "", "shell": "echo ${HOME}", "label": "a: b #c"},
		},
		{
			name: "Case 3: no value and no default",
			err:  "no value for size; pass -var name=value",
		},
		{
			name: "Case 4: a var no workflow uses",
			vars: Vars{"size": "1", "gangsize": "2"},
			err:  "no workflow uses -var gangsize",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workflow.yaml")
			require.NoError(t, os.WriteFile(path, []byte(workflow), 0o600))

			c, err := NewFromPaths(path, tc.vars)
			if len(tc.err) != 0 {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.params, c[0].Tasks[0].Params)
		})
	}
}

func TestWorkflowVarsDir(t *testing.T) {
	dir := t.TempDir()
	for name, ref := range map[string]string{"a.yaml": "${a}", "b.yaml": "${b} ${b}"} {
		data := "name: " + name + "\ntasks:\n  - id: t\n    type: Sleep\n    params:\n      v: " + ref + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600))
	}

	// Each var is used by only one file; the unused check must span the whole directory.
	c, err := NewFromPaths(dir, Vars{"a": "1", "b": "2"})
	require.NoError(t, err)
	require.Len(t, c, 2)

	_, err = NewFromPaths(dir, Vars{"a": "1"})
	require.ErrorContains(t, err, "no value for b;")
}

func TestVarsSet(t *testing.T) {
	vars := Vars{}
	require.NoError(t, vars.Set("racks=a=b"))
	require.Equal(t, "racks=a=b", vars.String())
	require.Error(t, vars.Set("racks"))
	require.Error(t, vars.Set("1racks=8"))
}
