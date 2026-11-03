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

		switch val := val.(type) {
		case map[interface{}]interface{}, map[string]interface{}:
			var o map[string]interface{}
			switch objVal := objVal.(type) {
			case map[interface{}]interface{}:
				o = convert(objVal)
			case map[string]interface{}:
				o = objVal
			default:
				return false // subset expects a map but the object holds another value
			}
			var s map[string]interface{}
			switch val := val.(type) {
			case map[interface{}]interface{}:
				s = convert(val)
			case map[string]interface{}:
				s = val
			}
			if !IsSubset(o, s) {
				return false
			}

		case []interface{}:
			if _, ok := objVal.([]interface{}); !ok {
				return false
			}

		case int, int32, int64:
			objInt, ok := toInt64(objVal)
			subInt, _ := toInt64(val)
			if !ok || objInt != subInt {
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

// toInt64 converts the integer types found in unmarshalled YAML. It reports
// false for anything else, e.g. a null or fractional value in the live
// object, which is a mismatch rather than a panic.
func toInt64(val interface{}) (int64, bool) {
	switch v := val.(type) {
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}
