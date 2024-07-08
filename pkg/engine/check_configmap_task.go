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
	"k8s.io/client-go/kubernetes"

	"github.com/NVIDIA/knavigator/pkg/config"
)

// CheckConfigmapTask represents a task that checks content of a configmap.
type CheckConfigmapTask struct {
	BaseTask
	checkConfigmapTaskParams

	client *kubernetes.Clientset
}

type checkConfigmapTaskParams struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Data      map[string]string `yaml:"data"`
	Op        string            `yaml:"op"`
}

// newCheckConfigmapTask initializes and returns CheckPodTask
func newCheckConfigmapTask(client *kubernetes.Clientset, cfg *config.Task) (*CheckConfigmapTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: Kubernetes client is not set", cfg.Type, cfg.ID)
	}

	task := &CheckConfigmapTask{
		BaseTask: BaseTask{
			taskType: cfg.Type,
			taskID:   cfg.ID,
		},
		client: client,
	}

	if err := task.validate(cfg.Params); err != nil {
		return nil, err
	}

	return task, nil
}

func (task *CheckConfigmapTask) validate(params map[string]interface{}) error {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}
	if err = yaml.Unmarshal(data, &task.checkConfigmapTaskParams); err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}

	if len(task.Name) == 0 || len(task.Namespace) == 0 {
		return fmt.Errorf("%s: must specify name and namespace", task.ID())
	}

	switch task.Op {
	case OpCmpEqual, OpCmpSubset:
		// nop
	default:
		return fmt.Errorf("%s: invalid configmap operation %s; supported: %s, %s", task.ID(), task.Op, OpCmpEqual, OpCmpSubset)
	}

	return nil
}

// Exec implements Runnable interface
func (task *CheckConfigmapTask) Exec(ctx context.Context) error {
	cm, err := task.client.CoreV1().ConfigMaps(task.Namespace).Get(ctx, task.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("%s: failed to get configmap %s/%s: %v", task.ID(), task.Namespace, task.Name, err)
	}

	return task.compareConfigMaps(cm.Data)
}

func (task *CheckConfigmapTask) compareConfigMaps(data map[string]string) error {
	if (task.Op == OpCmpEqual && len(task.Data) != len(data)) || len(task.Data) > len(data) {
		return fmt.Errorf("%s: configmap %s/%s has %d items; expected %d", task.ID(), task.Namespace, task.Name, len(data), len(task.Data))
	}

	for key, expected := range task.Data {
		actual, ok := data[key]
		if !ok {
			return fmt.Errorf("%s: configmap %s/%s does not have key %s", task.ID(), task.Namespace, task.Name, key)
		}
		if expected != actual {
			return fmt.Errorf("%s: configmap %s/%s does not match value for key %s", task.ID(), task.Namespace, task.Name, key)
		}
	}

	return nil
}
