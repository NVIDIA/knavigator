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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

// UpdateObjTask represents task that updates object state and status
type UpdateObjTask struct {
	ObjStateTask
}

func newUpdateObjTask(client *dynamic.DynamicClient, accessor ObjInfoAccessor, cfg *config.Task) (*UpdateObjTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: DynamicClient is not set", cfg.Type, cfg.ID)
	}

	task := &UpdateObjTask{
		ObjStateTask: ObjStateTask{
			BaseTask: BaseTask{
				taskType: cfg.Type,
				taskID:   cfg.ID,
			},
			client:   client,
			accessor: accessor,
		},
	}

	if err := task.validate(cfg.Params); err != nil {
		return nil, err
	}

	return task, nil
}

// Exec implements Runnable interface
func (task *UpdateObjTask) Exec(ctx context.Context) error {
	info, err := task.accessor.GetObjInfo(task.RefTaskID)
	if err != nil {
		return err
	}

	patch, err := utils.NewPatchData(task.State)
	if err != nil {
		return fmt.Errorf("%s: failed to generate patch: %v", task.ID(), err)
	}

	gvr := info.GVR[task.Index]
	for _, name := range info.Names {
		if patch.Root != nil {
			_, err = task.client.Resource(gvr).Namespace(info.Namespace).Patch(ctx, name, types.MergePatchType, patch.Root, metav1.PatchOptions{})
			if err != nil {
				return fmt.Errorf("%s: failed to patch %s %s: %v", task.ID(), gvr.Resource, name, err)
			}
		}
		if patch.Status != nil {
			_, err = task.client.Resource(gvr).Namespace(info.Namespace).Patch(ctx, name, types.MergePatchType, patch.Root, metav1.PatchOptions{}, "status")
			if err != nil {
				return fmt.Errorf("%s: failed to patch status %s %s: %v", task.ID(), gvr.Resource, name, err)
			}
		}
	}

	return nil
}
