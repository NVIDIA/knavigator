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

import "encoding/json"

// PatchData contains patches for root and status subresources.
type PatchData struct {
	Root   []byte
	Status []byte
}

// NewPatchData initializes PatchData object
func NewPatchData(state map[string]interface{}) (*PatchData, error) {
	var err error
	patch := &PatchData{}

	root := make(map[string]interface{})

	if v, ok := state["metadata"]; ok {
		root["metadata"] = v
	}
	if v, ok := state["spec"]; ok {
		root["spec"] = v
	}

	if len(root) != 0 {
		if patch.Root, err = json.Marshal(root); err != nil {
			return nil, err
		}
	}

	if v, ok := state["status"]; ok {
		status := map[string]interface{}{"status": v}
		if patch.Status, err = json.Marshal(status); err != nil {
			return nil, err
		}
	}

	return patch, nil
}
