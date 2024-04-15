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

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNameSelector(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		names []string
		match map[string]bool
		err   string
	}{
		{
			name: "Case 1a: list with names",
			input: `
list:
  patterns:
  - name1
  - name2
`,
			names: []string{"name1", "name2"},
			match: map[string]bool{"name1": true, "noname": false},
		},
		{
			name: "Case 1b: list with patterns",
			input: `
list:
  patterns:
  - name{{.key1}}subname{{.key2}}
  - name{{.key2}}subname{{.key1}}
  params:
    key1: 1
    key2: Two
`,
			names: []string{"name1subnameTwo", "nameTwosubname1"},
			match: map[string]bool{"nameTwosubname1": true, "noname": false},
		},
		{
			name: "Case 2: range only",
			input: `
range:
  pattern: "name{{.id}}worker{{._INDEX_}}"
  ranges: ["2-3","5"]
  params:
    id: 123
`,
			names: []string{"name123worker2", "name123worker3", "name123worker5"},
			match: map[string]bool{"name123worker2": true, "noname": false},
		},
		{
			name: "Case 3: all",
			input: `
list:
  patterns:
  - name{{.key1}}subname{{.key2}}
  - name3
  params:
    key1: 1
    key2: Two
range:
  pattern: "worker{{._INDEX_}}"
  ranges: ["5", "12-14", "8"]
regexp: "^name4[0-9]$"
`,
			names: []string{"name1subnameTwo", "name3", "worker5", "worker12", "worker13", "worker14", "worker8"},
			match: map[string]bool{"name3": true, "noname": false, "worker13": true, "name45": true},
		},
		{
			name: "Case 4a: invalid list template",
			input: `
list:
  patterns:
  - name{{{.key1}}
  params:
    key1: 1
`,
			err: "failed to parse template name{{{.key1}}: template: name:1: unexpected \"{\" in command",
		},
		{
			name: "Case 4b: missing list patterns",
			input: `
list:
  params:
    key1: 1
`,
			err: "missing patterns in name list",
		},
		{
			name: "Case 5a: missing range pattern",
			input: `
range:
  ranges: ["5", "12-14", "8"]
`,
			err: "missing pattern in name range",
		},
		{
			name: "Case 5b: missing ranges",
			input: `
range:
  pattern: "worker{{._INDEX_}}"
`,
			err: "missing ranges in name range",
		},
		{
			name: "Case 5c: invalid range template",
			input: `
range:
  pattern: "worker{{{._INDEX_}}"
  ranges: ["5", "12-14", "8"]
`,
			err: "failed to parse template worker{{{._INDEX_}}: template: name:1: unexpected \"{\" in command",
		},
		{
			name: "Case 6a: invalid range template",
			input: `
range:
  pattern: "worker{{._INDEX_}}"
  ranges: ["", "12-14", "8"]
`,
			err: `invalid range "" in name range`,
		},
		{
			name: "Case 6b: invalid range values",
			input: `
range:
  pattern: "worker{{._INDEX_}}"
  ranges: ["14-12", "8"]
`,
			err: `invalid range "14-12" in name range`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var ns NameSelector
			err := yaml.Unmarshal([]byte(tc.input), &ns)
			require.NoError(t, err)
			err = ns.Finalize()
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)

				iter := ns.Iter()
				names := []string{}
				for iter.HasNext() {
					names = append(names, iter.GetNext())
				}
				require.Equal(t, tc.names, names)

				matcher := ns.Matcher()
				for name, match := range tc.match {
					require.Equal(t, match, matcher.IsMatch(name), "mismatch for %s", name)
				}
			}
		})
	}
}
