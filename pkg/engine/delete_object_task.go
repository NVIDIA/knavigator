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
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type DeleteObjTask struct {
	BaseTask
	deleteObjTaskParams

	client *dynamic.DynamicClient
	getter ObjInfoAccessor
}

type deleteObjTaskParams struct {
	RefTaskID string `yaml:"refTaskId"`
}

// newDeleteObjTask initializes and returns DeleteObjTask
func newDeleteObjTask(log logr.Logger, client *dynamic.DynamicClient, getter ObjInfoAccessor, cfg *config.Task) (*DeleteObjTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: DynamicClient is not set", cfg.Type, cfg.ID)
	}

	task := &DeleteObjTask{
		BaseTask: BaseTask{
			log:      log,
			taskType: cfg.Type,
			taskID:   cfg.ID,
		},
		client: client,
		getter: getter,
	}

	if err := task.validate(cfg.Params); err != nil {
		return nil, err
	}

	return task, nil
}

// validate initializes and validates parameters for DeleteObjTask
func (task *DeleteObjTask) validate(params map[string]interface{}) (err error) {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}
	if err = yaml.Unmarshal(data, &task.deleteObjTaskParams); err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}

	if len(task.RefTaskID) == 0 {
		return fmt.Errorf("%s: missing parameter 'refTaskId'", task.ID())
	}

	return
}

// Exec implements Runnable interface
func (task *DeleteObjTask) Exec(ctx context.Context) error {
	info, err := task.getter.GetObjInfo(task.RefTaskID)
	if err != nil {
		return err
	}

	task.log.V(4).Info("Deleting objects", "GVR", info.GVR.String(), "names", info.Names)

	for _, name := range info.Names {
		prop := v1.DeletePropagationBackground
		opt := v1.DeleteOptions{
			PropagationPolicy: &prop,
		}
		err = task.client.Resource(info.GVR).Namespace(info.Namespace).Delete(ctx, name, opt)
		if err != nil {
			return err
		}
	}
	return nil
}
