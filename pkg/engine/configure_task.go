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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type ConfigureTask struct {
	BaseTask
	configureTaskParams

	client *kubernetes.Clientset
}

type configureTaskParams struct {
	Nodes   []virtualNode `yaml:"nodes"`
	Timeout time.Duration `yaml:"timeout"`
}

type virtualNode struct {
	Type        string              `yaml:"type" json:"type"`
	Count       int                 `yaml:"count" json:"count"`
	Annotations map[string]string   `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	Labels      map[string]string   `yaml:"labels,omitempty" json:"labels,omitempty"`
	Conditions  []map[string]string `yaml:"conditions,omitempty" json:"conditions,omitempty"`
}

func newConfigureTask(log logr.Logger, client *kubernetes.Clientset, cfg *config.Task) (*ConfigureTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: Kubernetes client is not set", cfg.Type, cfg.ID)
	}

	task := &ConfigureTask{
		BaseTask: BaseTask{
			log:      log,
			taskType: TaskConfigure,
			taskID:   cfg.ID,
		},
		client: client,
	}

	if err := task.validate(cfg.Params); err != nil {
		return nil, err
	}

	return task, nil
}

// validate initializes and validates parameters for ConfigureTask
func (task *ConfigureTask) validate(params map[string]interface{}) error {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to parse parameters in %s task %s: %v", task.taskType, task.taskID, err)
	}
	if err = yaml.Unmarshal(data, &task.configureTaskParams); err != nil {
		return fmt.Errorf("failed to parse parameters in %s task %s: %v", task.taskType, task.taskID, err)
	}

	if task.Timeout == 0 {
		return fmt.Errorf("%s: missing parameter 'timeout'", task.ID())
	}

	return nil
}

// Exec implements Runnable interface
func (task *ConfigureTask) Exec(ctx context.Context) (err error) {
	ctx, cancel := context.WithTimeout(ctx, task.Timeout)
	defer cancel()

	stop := make(chan error)
	defer close(stop)

	go func() {
		stop <- task.updateVirtualNodes(ctx)
	}()

	select {
	case err := <-stop:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (task *ConfigureTask) updateVirtualNodes(ctx context.Context) error {
	if len(task.Nodes) == 0 {
		return nil
	}

	nodeExpr, err := nodes2json(task.Nodes)
	if err != nil {
		return err
	}

	// update helm repo
	args := []string{"repo", "add", "--force-update", "knavigator", "https://nvidia.github.io/knavigator/helm-charts"}

	task.log.V(4).Info("Updating helm repo")

	if err = runCommand(ctx, task.log, "helm", args); err != nil {
		return err
	}

	// upgrade helm chart
	args = []string{"upgrade", "--install", "virtual-nodes", "knavigator/virtual-nodes",
		"--wait", "--set-json", nodeExpr}

	task.log.V(4).Info("Updating nodes", "cmd", append([]string{"helm"}, args...))

	return runCommand(ctx, task.log, "helm", args)
}

func nodes2json(nodes []virtualNode) (string, error) {
	data, err := json.Marshal(nodes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("nodes=%s", string(data)), nil
}

func runCommand(ctx context.Context, log logr.Logger, exe string, args []string) error {
	command := exec.CommandContext(ctx, exe, args...)

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		log.Error(err, "failed to run command",
			"stdout", stdout.String(), "stderr", stderr.String())
		return err
	}

	log.V(4).Info(stdout.String())

	return nil
}
