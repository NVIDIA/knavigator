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
		{
			name:   "Case 6: null object value against an integer subset",
			obj:    map[string]interface{}{"replicas": nil},
			subset: map[string]interface{}{"replicas": 3},
			match:  false,
		},
		{
			name:   "Case 7: fractional object value against an integer subset",
			obj:    map[string]interface{}{"progress": 0.5},
			subset: map[string]interface{}{"progress": 1},
			match:  false,
		},
		{
			name:   "Case 8: integer match across int types",
			obj:    map[string]interface{}{"replicas": int64(3)},
			subset: map[string]interface{}{"replicas": 3},
			match:  true,
		},
		{
			name:   "Case 9: empty map subset does not match a null object value",
			obj:    map[string]interface{}{"status": nil},
			subset: map[string]interface{}{"status": map[string]interface{}{}},
			match:  false,
		},
		{
			name:   "Case 10: map subset against a non-map object value",
			obj:    map[string]interface{}{"status": "ready"},
			subset: map[string]interface{}{"status": map[string]interface{}{"active": 3}},
			match:  false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.match, IsSubset(tc.obj, tc.subset))
		})
	}
}

func TestToInt64(t *testing.T) {
	testCases := []struct {
		name string
		val  interface{}
		ref  int64
		ok   bool
	}{
		{
			name: "Case 1: int",
			val:  int(5),
			ref:  5,
			ok:   true,
		},
		{
			name: "Case 2: int32",
			val:  int32(88),
			ref:  88,
			ok:   true,
		},
		{
			name: "Case 3: int64",
			val:  int64(101),
			ref:  101,
			ok:   true,
		},
		{
			name: "Case 4: nil is not an integer",
			val:  nil,
			ok:   false,
		},
		{
			name: "Case 5: float64 is not an integer",
			val:  0.5,
			ok:   false,
		},
		{
			name: "Case 6: string is not an integer",
			val:  "5",
			ok:   false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ref, ok := toInt64(tc.val)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.ref, ref)
		})
	}
}
