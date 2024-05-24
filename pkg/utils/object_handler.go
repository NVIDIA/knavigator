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

func IsSubset(obj, subset map[string]interface{}) bool {
	for key, val := range subset {
		objVal, ok := obj[key]
		if val != nil && !ok { // present in subset but absent in obj
			return false
		}
		if val == nil && ok { // should be absent in obj but present
			return false
		}

		switch val.(type) {
		case map[interface{}]interface{}, map[string]interface{}:
			var o, s map[string]interface{}
			if objVal, ok := obj[key].(map[interface{}]interface{}); ok {
				o = convert(objVal)
			}
			if objVal, ok := obj[key].(map[string]interface{}); ok {
				o = objVal
			}
			if subsetVal, ok := val.(map[interface{}]interface{}); ok {
				s = convert(subsetVal)
			}
			if subsetVal, ok := val.(map[string]interface{}); ok {
				s = subsetVal
			}
			if !IsSubset(o, s) {
				return false
			}

		case []interface{}:
			if _, ok := obj[key].([]interface{}); !ok {
				return false
			}

		case int, int32, int64:
			if toInt64(objVal) != toInt64(val) {
				return false
			}

		default:
			if objVal != val {
				return false
			}
		}
	}
	return true
}

func convert(in map[interface{}]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for k, v := range in {
		out[k.(string)] = v
	}
	return out
}

func toInt64(val interface{}) int64 {
	switch v := val.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	default:
		return val.(int64)
	}
}
