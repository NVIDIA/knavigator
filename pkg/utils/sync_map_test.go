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

func TestSyncMap(t *testing.T) {
	m := NewSyncMap()
	m.Set(5, 15)
	m.Set(6, 16)
	m.Set(7, 17)
	require.Equal(t, 3, m.Size())

	v, ok := m.Get(6)
	require.True(t, ok)
	require.Equal(t, 16, v)

	_, ok = m.Get(4)
	require.False(t, ok)

	sz := m.Delete(6)
	require.Equal(t, 2, sz)

	_, ok = m.Get(6)
	require.False(t, ok)

	m.Delete(5)
	require.Equal(t, []interface{}{7}, m.Keys())
}
