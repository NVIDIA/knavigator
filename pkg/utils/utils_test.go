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
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/klog/v2"
)

type dummyValue int

func (v *dummyValue) Set(_ string) error { return nil }
func (v *dummyValue) String() string     { return "" }

func TestFlag2Verbosity(t *testing.T) {
	var (
		level klog.Level = 4
		dummy dummyValue
	)

	testCases := []struct {
		name  string
		val   flag.Value
		level int
	}{
		{
			name:  "Case 1: no value",
			level: 0,
		},
		{
			name:  "Case 2: unexpected type",
			val:   &dummy,
			level: 0,
		},
		{
			name:  "Case 3: valid type",
			val:   &level,
			level: 4,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.level, Flag2Verbosity(&flag.Flag{Value: tc.val}))
		})
	}
}

func TestGenerateNames(t *testing.T) {
	testCases := []struct {
		name    string
		pattern string
		size    int
		params  map[string]interface{}
		names   []string
		err     string
	}{
		{
			name:  "Case 1: empty pattern",
			size:  2,
			names: []string{"", ""},
		},
		{
			name:    "Case 2: plain text",
			pattern: "name",
			size:    1,
			params:  make(map[string]interface{}),
			names:   []string{"name"},
		},
		{
			name:    "Case 3: plain text",
			pattern: "name{{._ENUM_}}",
			size:    3,
			params:  make(map[string]interface{}),
			names:   []string{"name6", "name7", "name8"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			SetObjectID(5)
			names, err := GenerateNames(tc.pattern, tc.size, tc.params)
			if len(tc.err) != 0 {
				require.EqualError(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.names, names)
			}
		})
	}
}
