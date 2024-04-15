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
)

func TestIsSubset(t *testing.T) {
	obj := map[string]interface{}{
		"runPolicy": map[interface{}]interface{}{
			"coScheduling": false,
		},
		"template": map[interface{}]interface{}{
			"metadata": map[interface{}]interface{}{
				"annotations": map[interface{}]interface{}{
					"orgName":  "orgName",
					"teamName": "teamName",
					"userName": "tester",
				},
				"labels": map[interface{}]interface{}{
					"obj-name": "1",
					"instance": "lnx2000",
				},
			},
			"spec": map[interface{}]interface{}{
				"nodeSelector": map[interface{}]interface{}{
					"nodeGroup": "gpu",
				},
				"restartPolicy": "Never",
			},
		},
	}

	testCases := []struct {
		name   string
		obj    map[string]interface{}
		subset map[string]interface{}
		match  bool
	}{
		{
			name: "Case 1: match",
			subset: map[string]interface{}{
				"template": map[interface{}]interface{}{
					"metadata": map[interface{}]interface{}{
						"annotations": map[interface{}]interface{}{
							"userName": "tester",
						},
						"labels": map[interface{}]interface{}{
							"instance": "lnx2000",
							"userID":   nil,
						},
					},
				},
			},
			obj:   obj,
			match: true,
		},
		{
			name: "Case 2: string mismatch",
			subset: map[string]interface{}{
				"template": map[interface{}]interface{}{
					"spec": map[interface{}]interface{}{
						"nodeSelector": map[interface{}]interface{}{
							"nodeGroup": "cpu",
						},
					},
				},
			},
			obj:   obj,
			match: false,
		},
		{
			name: "Case 3: bool mismatch",
			subset: map[string]interface{}{
				"runPolicy": map[interface{}]interface{}{
					"coScheduling": true,
				},
			},
			obj:   obj,
			match: false,
		},
		{
			name: "Case 4: should be absent but present",
			subset: map[string]interface{}{
				"template": map[interface{}]interface{}{
					"metadata": map[interface{}]interface{}{
						"labels": map[interface{}]interface{}{
							"instance": nil,
						},
					},
				},
			},
			obj:   obj,
			match: false,
		},
		{
			name: "Case 5: should be present but absent",
			subset: map[string]interface{}{
				"template": map[interface{}]interface{}{
					"metadata": map[interface{}]interface{}{
						"labels": map[interface{}]interface{}{
							"userID": 5,
						},
					},
				},
			},
			obj:   obj,
			match: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.match, IsSubset(tc.obj, tc.subset))
		})
	}
}
