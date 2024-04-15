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
	"fmt"
	"sync"
)

// SyncMap implements a map with synchronous access
type SyncMap struct {
	mutex sync.Mutex
	data  map[interface{}]interface{}
}

// NewSyncMap returns an empty SyncMap
func NewSyncMap() *SyncMap {
	return &SyncMap{
		data: make(map[interface{}]interface{}),
	}
}

// Set sets a key:value pair
func (m *SyncMap) Set(key interface{}, val interface{}) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data[key] = val
}

// Get return a value for a key (first returned argument) if found (second returned argument)
func (m *SyncMap) Get(key interface{}) (interface{}, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	val, ok := m.data[key]
	return val, ok
}

// Delete deletes a map entry specified by key, and returns updated number of elements in the map
func (m *SyncMap) Delete(key interface{}) int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.data, key)
	return len(m.data)
}

// Size returns number of elements in the map
func (m *SyncMap) Size() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return len(m.data)
}

// Keys returns array of map keys
func (m *SyncMap) Keys() []interface{} {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	arr := make([]interface{}, 0, len(m.data))
	for key := range m.data {
		arr = append(arr, key)
	}

	return arr
}

// String prints the map
func (m *SyncMap) String() string {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return fmt.Sprintf("%v", m.data)
}
