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

package engine

import (
	"fmt"

	"gopkg.in/yaml.v3"
	"k8s.io/client-go/dynamic"
)

// ObjStateTask represents a base structure for object manipulation.
type ObjStateTask struct {
	BaseTask
	StateParams

	client   *dynamic.DynamicClient
	accessor ObjInfoAccessor
}

// validate initializes and validates parameters for ObjStateTask
func (task *ObjStateTask) validate(params map[string]interface{}) error {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}
	if err = yaml.Unmarshal(data, &task.StateParams); err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}

	if len(task.RefTaskID) == 0 {
		return fmt.Errorf("%s: missing parameter 'refTaskId'", task.ID())
	}

	if len(task.State) == 0 {
		return fmt.Errorf("%s: missing parameter 'state'", task.ID())
	}

	return nil
}
