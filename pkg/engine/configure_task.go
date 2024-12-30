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
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type ConfigureTask struct {
	BaseTask
	configureTaskParams

	client *kubernetes.Clientset
}

type configureTaskParams struct {
	Nodes              []virtualNode        `yaml:"nodes"`
	Namespaces         []namespace          `yaml:"namespaces"`
	ConfigMaps         []configmap          `yaml:"configmaps"`
	PriorityClasses    []priorityClass      `yaml:"priorityClasses"`
	DeploymentRestarts []*deploymentRestart `yaml:"deploymentRestarts"`

	Timeout time.Duration `yaml:"timeout"`
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

type priorityClass struct {
	Name  string `yaml:"name"`
	Value *int32 `yaml:"value,omitempty"`
	Op    string `yaml:"op"`
}

type deploymentRestart struct {
	Name      string            `yaml:"name,omitempty"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

func newConfigureTask(client *kubernetes.Clientset, cfg *config.Task) (*ConfigureTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: Kubernetes client is not set", cfg.Type, cfg.ID)
	}

	task := &ConfigureTask{
		BaseTask: BaseTask{
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

	for _, pc := range task.PriorityClasses {
		switch pc.Op {
		case OpCreate:
			if pc.Value == nil {
				return fmt.Errorf("%s: must provide value when creating PriorityClass", task.ID())
			}
		case OpDelete:
			// nop
		default:
			return fmt.Errorf("%s: invalid PriorityClass operation %s; supported: %s, %s", task.ID(), pc.Op, OpCreate, OpDelete)
		}
	}

	for _, dr := range task.DeploymentRestarts {
		if len(dr.Namespace) == 0 {
			return fmt.Errorf("%s: must provide namespace when restarting deployment", task.ID())
		}
		if (len(dr.Name) != 0 && len(dr.Labels) != 0) || (len(dr.Name) == 0 && len(dr.Labels) == 0) {
			return fmt.Errorf("%s: must provide either name or labels when restarting deployment", task.ID())
		}
		if len(dr.Name) == 0 {
			for key := range dr.Labels {
				if len(key) == 0 {
					return fmt.Errorf("%s: must provide non-empty label name when restarting deployment", task.ID())
				}
			}
		}
	}

	if task.Timeout == 0 {
		return fmt.Errorf("%s: missing parameter 'timeout'", task.ID())
	}

	return nil
}

// Exec implements Runnable interface
func (task *ConfigureTask) Exec(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, task.Timeout)
	defer cancel()

	errs := make(chan error)
	var wg sync.WaitGroup
	wg.Add(4)

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
		errs <- task.updatePriorityClasses(ctx)
	}()

	go func() {
		defer wg.Done()
		errs <- task.updateConfigmaps(ctx)
	}()

	go func() {
		defer wg.Done()
		errs <- task.updateVirtualNodes(ctx)
	}()

	var err error
	for e := range errs {
		if e != nil {
			log.Errorf("configuration error: %v", e)
			err = e
		}
	}

	if err != nil {
		return err
	}

	for _, dr := range task.DeploymentRestarts {
		if err = task.restartDeployment(ctx, dr); err != nil {
			return err
		}
	}

	return nil
}

func (task *ConfigureTask) updateNamespaces(ctx context.Context) error {
	for _, ns := range task.Namespaces {
		log.Infof("%s namespace %s", ns.Op, ns.Name)
		switch ns.Op {
		case OpCreate:
			_, err := task.client.CoreV1().Namespaces().Get(ctx, ns.Name, metav1.GetOptions{})
			if err == nil {
				log.Infof("Namespace %s already exist", ns.Name)
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
			log.Infof("Namespace %s deleted", ns.Name)
		}
	}

	return nil
}

func (task *ConfigureTask) updatePriorityClasses(ctx context.Context) error {
	for _, pc := range task.PriorityClasses {
		log.Infof("%s PriorityClass %s", pc.Op, pc.Name)

		switch pc.Op {
		case OpCreate:
			newObj := &schedulingv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: pc.Name,
				},
				Value: *pc.Value,
			}

			curObj, err := task.client.SchedulingV1().PriorityClasses().Get(ctx, pc.Name, metav1.GetOptions{})
			if err == nil {
				if curObj.Value == newObj.Value {
					log.Infof("PriorityClass %s with value %d already exist", curObj.Name, curObj.Value)
				} else {
					log.Infof("Updating PriorityClass %s with value %d", newObj.Name, newObj.Value)
					_, err = task.client.SchedulingV1().PriorityClasses().Update(ctx, newObj, metav1.UpdateOptions{})
				}
			} else if errors.IsNotFound(err) {
				log.Infof("Creating PriorityClass %s with value %d", newObj.Name, newObj.Value)
				_, err = task.client.SchedulingV1().PriorityClasses().Create(ctx, newObj, metav1.CreateOptions{})
			}
			if err != nil {
				return fmt.Errorf("%s: failed to create PriorityClass %s: %v", task.ID(), pc.Name, err)
			}

		case OpDelete:
			err := task.client.SchedulingV1().PriorityClasses().Delete(ctx, pc.Name, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("%s: failed to delete PriorityClass %s: %v", task.ID(), pc.Name, err)
			}
			log.Infof("PriorityClass %s deleted", pc.Name)
		}
	}

	return nil
}

func (task *ConfigureTask) updateConfigmaps(ctx context.Context) error {
	for _, cm := range task.ConfigMaps {
		log.Infof("%s configmap %s", cm.Op, cm.Name)
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
			log.Infof("Configmap %s %sd", cm.Name, op)

		case OpDelete:
			_, err := task.client.CoreV1().ConfigMaps(cm.Namespace).Get(ctx, cm.Name, metav1.GetOptions{})
			if err == nil {
				err = task.client.CoreV1().ConfigMaps(cm.Namespace).Delete(ctx, cm.Name, metav1.DeleteOptions{})
			} else if errors.IsNotFound(err) {
				log.V(4).Infof("Configmap %s does not exist; nothing to delete", cm.Name)
				err = nil
			}
			if err != nil {
				return fmt.Errorf("%s: failed to delete configmap %s: %v", task.ID(), cm.Name, err)
			}
			log.Infof("Configmap %s deleted", cm.Name)
		}
	}

	return nil
}

func (task *ConfigureTask) restartDeployment(ctx context.Context, dr *deploymentRestart) error {
	dClient := task.client.AppsV1().Deployments(dr.Namespace)

	dName := dr.Name
	if len(dName) == 0 {
		labels := make([]string, 0, len(dr.Labels))
		for key, val := range dr.Labels {
			labels = append(labels, key+"="+val)
		}
		lbl := strings.Join(labels, ",")

		list, err := dClient.List(ctx, metav1.ListOptions{LabelSelector: lbl})
		if err != nil {
			log.InfoS("Warning: skipping restart of deployment", "labels", lbl, "error", err.Error())
			return nil
		}

		if len(list.Items) == 0 {
			log.InfoS("Warning: no deployment to restart", "labels", lbl)
			return nil
		}

		if len(list.Items) != 1 {
			return fmt.Errorf("expected 1 deployment with labels %s, not %d", lbl, len(list.Items))
		}

		dName = list.Items[0].Name
	}
	log.Infof("Restarting deployment %s", dName)

	update := fmt.Sprintf(`{"spec": {"template": {"metadata": {"annotations": {"kubectl.kubernetes.io/restartedAt": "%s"}}}}}`,
		time.Now().Format("2006-01-02T15:04:05-07:00"))

	_, err := dClient.Patch(ctx, dName, k8stypes.StrategicMergePatchType, []byte(update), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment %s: %s", dName, err.Error())
	}

	delay := 5 * time.Second
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			d, err := dClient.Get(ctx, dName, metav1.GetOptions{})
			if err != nil {
				log.Errorf("failed to get status for deployment %s : %v", dName, err)
			} else if d.Status.UnavailableReplicas != 0 {
				log.V(4).Infof("Restarting deployment %s: %d unavailable replicas", dName, d.Status.UnavailableReplicas)
			} else {
				log.Infof("Restarted deployment %s", dName)
				return nil
			}
			timer.Reset(delay)

		case <-ctx.Done():
			return ctx.Err()
		}
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

	log.V(4).Infof("Updating helm repo")

	if err = runCommand(ctx, "helm", args); err != nil {
		return err
	}

	// upgrade helm chart
	args = []string{"upgrade", "--install", "virtual-nodes", "knavigator/virtual-nodes",
		"--wait", "--set-json", nodeExpr}

	log.V(4).Infof("Updating nodes with %v", append([]string{"helm"}, args...))

	return runCommand(ctx, "helm", args)
}

func nodes2json(nodes []virtualNode) (string, error) {
	data, err := json.Marshal(nodes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("nodes=%s", string(data)), nil
}

func runCommand(ctx context.Context, exe string, args []string) error {
	command := exec.CommandContext(ctx, exe, args...)

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		log.Errorf("failed to run command: err:%v stdout:%s stderr:%s", err, stdout.String(), stderr.String())
		return err
	}

	log.V(4).Info(stdout.String())

	return nil
}
