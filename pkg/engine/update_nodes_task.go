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

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

// UpdateNodesTask represents UpdateNodes task.
// This task sets labels specified in params.Labels to the nodes specified in params.Selector
type UpdateNodesTask struct {
	BaseTask
	nodeStateParams

	client *kubernetes.Clientset
}

// nodeStateParams contains parameters set by the user.
type nodeStateParams struct {
	StateParams `yaml:",inline"`

	Selectors []map[string]string `yaml:"selectors"`
}

func newUpdateNodesTask(client *kubernetes.Clientset, cfg *config.Task) (*UpdateNodesTask, error) {
	if client == nil {
		return nil, fmt.Errorf("kubernetes clientset not set")
	}

	task := &UpdateNodesTask{
		BaseTask: BaseTask{
			taskType: cfg.Type,
			taskID:   cfg.ID,
		},
		client: client,
	}

	if err := task.validate(task.taskType, task.taskID, cfg.Params); err != nil {
		return nil, err
	}

	return task, nil
}

func (p *nodeStateParams) validate(taskType, taskID string, params map[string]interface{}) error {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to parse parameters in %s task %s: %v", taskType, taskID, err)
	}
	if err = yaml.Unmarshal(data, p); err != nil {
		return fmt.Errorf("failed to parse parameters in %s task %s: %v", taskType, taskID, err)
	}

	if len(p.Selectors) == 0 {
		return fmt.Errorf("missing node selectors in %s task %s", taskType, taskID)
	}

	if len(p.State) == 0 {
		return fmt.Errorf("missing state parameters in %s task %s", taskType, taskID)
	}

	return nil
}

// Exec implements Runnable interface
func (task *UpdateNodesTask) Exec(ctx context.Context) error {
	nodeClient := task.client.CoreV1().Nodes()
	nodeList, err := nodeClient.List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	patch, err := utils.NewPatchData(task.State)
	if err != nil {
		return fmt.Errorf("%s: failed to generate patch: %v", task.ID(), err)
	}

	for _, node := range nodeList.Items {
		for _, selector := range task.Selectors {
			if isMapSubset(node.Labels, selector) {
				if patch.Root != nil {
					if _, err := nodeClient.Patch(ctx, node.Name, types.MergePatchType, patch.Root, metav1.PatchOptions{}); err != nil {
						return err
					}
				}
				if patch.Status != nil {
					if _, err := nodeClient.PatchStatus(ctx, node.Name, patch.Status); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func isMapSubset(mapSet, mapSubset map[string]string) bool {
	for key, value := range mapSubset {
		if v, ok := mapSet[key]; !ok || v != value {
			return false
		}
	}
	return true
}
