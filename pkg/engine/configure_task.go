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
	"sync"
	"time"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type ConfigureTask struct {
	BaseTask
	configureTaskParams

	client *kubernetes.Clientset
}

type configureTaskParams struct {
	Nodes      []virtualNode `yaml:"nodes"`
	Namespaces []namespace   `yaml:"namespaces"`
	ConfigMaps []configmap   `yaml:"configmaps"`
	Timeout    time.Duration `yaml:"timeout"`
}

type virtualNode struct {
	Type        string              `yaml:"type" json:"type"`
	Count       int                 `yaml:"count" json:"count"`
	Annotations map[string]string   `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	Labels      map[string]string   `yaml:"labels,omitempty" json:"labels,omitempty"`
	Conditions  []map[string]string `yaml:"conditions,omitempty" json:"conditions,omitempty"`
}

type namespace struct {
	Name string `yaml:"name"`
	Op   string `yaml:"op"`
}

type configmap struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Data      map[string]string `yaml:"data"`
	Op        string            `yaml:"op"`
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
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}
	if err = yaml.Unmarshal(data, &task.configureTaskParams); err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}

	for _, ns := range task.Namespaces {
		switch ns.Op {
		case OpCreate, OpDelete:
			// nop
		default:
			return fmt.Errorf("%s: invalid namespace operation %s; supported: %s, %s", task.ID(), ns.Op, OpCreate, OpDelete)
		}
	}

	for _, cm := range task.ConfigMaps {
		switch cm.Op {
		case OpCreate, OpDelete:
			// nop
		default:
			return fmt.Errorf("%s: invalid configmap operation %s; supported: %s, %s", task.ID(), cm.Op, OpCreate, OpDelete)
		}
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

	errs := make(chan error)
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		wg.Wait()
		close(errs)
	}()

	go func() {
		defer wg.Done()
		errs <- task.updateNamespaces(ctx)
	}()

	go func() {
		defer wg.Done()
		errs <- task.updateConfigmaps(ctx)
	}()

	go func() {
		defer wg.Done()
		errs <- task.updateVirtualNodes(ctx)
	}()

	for e := range errs {
		if e != nil {
			task.log.Error(e, "configuration error")
			err = e
		}
	}

	return
}

func (task *ConfigureTask) updateNamespaces(ctx context.Context) error {
	for _, ns := range task.Namespaces {
		task.log.Info("Update namespace", "name", ns.Name, "op", ns.Op)
		switch ns.Op {
		case OpCreate:
			_, err := task.client.CoreV1().Namespaces().Get(ctx, ns.Name, metav1.GetOptions{})
			if err == nil {
				task.log.Info("Namespace already exist", "name", ns.Name)
			} else if errors.IsNotFound(err) {
				ns := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: ns.Name,
					},
				}
				_, err = task.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			}
			if err != nil {
				return fmt.Errorf("%s: failed to create namespace %s: %v", task.ID(), ns.Name, err)
			}

		case OpDelete:
			err := task.client.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("%s: failed to delete namespace %s: %v", task.ID(), ns.Name, err)
			}
			task.log.Info("Namespace deleted", "name", ns.Name)
		}
	}

	return nil
}

func (task *ConfigureTask) updateConfigmaps(ctx context.Context) error {
	for _, cm := range task.ConfigMaps {
		task.log.Info("Update configmap", "name", cm.Name, "op", cm.Op)
		switch cm.Op {
		case OpCreate:
			var op string
			cmap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cm.Name,
					Namespace: cm.Namespace,
				},
				Data: cm.Data,
			}
			_, err := task.client.CoreV1().ConfigMaps(cm.Namespace).Get(ctx, cm.Name, metav1.GetOptions{})
			if err == nil {
				op = "update"
				_, err = task.client.CoreV1().ConfigMaps(cm.Namespace).Update(ctx, cmap, metav1.UpdateOptions{})
			} else if errors.IsNotFound(err) {
				op = "create"
				_, err = task.client.CoreV1().ConfigMaps(cm.Namespace).Create(ctx, cmap, metav1.CreateOptions{})
			}
			if err != nil {
				return fmt.Errorf("%s: failed to %s configmap %s: %v", task.ID(), op, cm.Name, err)
			}
			task.log.Info("Configmap configured", "name", cm.Name, "op", op)

		case OpDelete:
			err := task.client.CoreV1().ConfigMaps(cm.Namespace).Delete(ctx, cm.Name, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("%s: failed to delete configmap %s: %v", task.ID(), cm.Name, err)
			}
			task.log.Info("Configmap deleted", "name", cm.Name)
		}
	}

	return nil
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
		log.Error(err, "failed to run command", "stdout", stdout.String(), "stderr", stderr.String())
		return err
	}

	log.V(4).Info(stdout.String())

	return nil
}
