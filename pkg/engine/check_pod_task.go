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
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

// CheckPodTask represents CheckPod task.
// A SubmitJob task launches 1 or more NGCJobs. These NGCJobs are associated with the task ID of the SubmitJob task.
// A CheckPod task accepts task ID of a previously executed SubmitJob and verifies that
// all pods started by all NGCJobs (in turn, started by the aforementioned SubmitJob task) have expected Pod.Status.Phase
type CheckPodTask struct {
	BaseTask
	checkPodTaskParams

	client   *kubernetes.Clientset
	accessor ObjInfoAccessor
}

type checkPodTaskParams struct {
	RefTaskID  string            `yaml:"refTaskId"`
	Status     string            `yaml:"status"`
	NodeLabels map[string]string `yaml:"nodeLabels"`
	Timeout    time.Duration     `yaml:"timeout"`
}

// newCheckPodTask initializes and returns CheckPodTask
func newCheckPodTask(client *kubernetes.Clientset, accessor ObjInfoAccessor, cfg *config.Task) (*CheckPodTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: Kubernetes client is not set", cfg.Type, cfg.ID)
	}

	task := &CheckPodTask{
		BaseTask: BaseTask{
			taskType: cfg.Type,
			taskID:   cfg.ID,
		},
		client:   client,
		accessor: accessor,
	}

	if err := task.validate(cfg.Params); err != nil {
		return nil, err
	}

	return task, nil
}

// validate initializes and validates parameters for CheckPodTask
func (task *CheckPodTask) validate(params map[string]interface{}) error {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}
	if err = yaml.Unmarshal(data, &task.checkPodTaskParams); err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}

	if len(task.RefTaskID) == 0 {
		return fmt.Errorf("%s: missing parameter 'refTaskId'", task.ID())
	}

	if len(task.Status) == 0 && len(task.NodeLabels) == 0 {
		return fmt.Errorf("%s: missing parameters 'status' and/or 'nodeLabels'", task.ID())
	}

	return nil
}

// Exec implements Runnable interface
func (task *CheckPodTask) Exec(ctx context.Context) error {
	info, err := task.accessor.GetObjInfo(task.RefTaskID)
	if err != nil {
		return err
	}

	if len(info.PodRegexp) == 0 {
		return fmt.Errorf("%s: no pods to check", task.ID())
	}

	if task.Timeout == 0 {
		return task.checkPods(ctx, info)
	}
	return task.watchPods(ctx, info)
}

func (task *CheckPodTask) checkPods(ctx context.Context, info *ObjInfo) error {
	list, err := task.client.CoreV1().Pods(info.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("%s: failed to list pods: %v", task.ID(), err)
	}

	re, err := utils.Exp2Regexp(info.PodRegexp)
	if err != nil {
		return fmt.Errorf("%s: %v", task.ID(), err)
	}

	var count int
	for i := range list.Items {
		pod := &list.Items[i]
		for _, r := range re {
			if r.MatchString(pod.Name) {
				log.V(4).Infof("Matched pod %s", pod.Name)
				count++

				status := string(pod.Status.Phase)
				if status != task.Status {
					return fmt.Errorf("%s: pod %s, status %s, expected %s", task.ID(), pod.Name, status, task.Status)
				}

				if err := task.verifyLabels(ctx, pod); err != nil {
					return err
				}
			}
		}
	}

	if count != info.PodCount {
		return fmt.Errorf("%s: verified %d pods, expected %d", task.ID(), count, info.PodCount)
	}

	return nil
}

// watchPods watches statuses of given pods and compares them with the expected status.
// The function runs until all statuses are equal to the expected one, or until the timeout, whichever comes first.
func (task *CheckPodTask) watchPods(ctx context.Context, info *ObjInfo) error {
	log.Infof("Create pod informer for %d pods with %s timeout", info.PodCount, task.Timeout.String())

	re, err := utils.Exp2Regexp(info.PodRegexp)
	if err != nil {
		return fmt.Errorf("%s: %v", task.ID(), err)
	}

	ctx, cancel := context.WithTimeout(ctx, task.Timeout)
	defer cancel()

	podMap := utils.NewSyncMap()

	errs := make(chan error)

	factory := informers.NewSharedInformerFactoryWithOptions(task.client, 30*time.Second, informers.WithNamespace(info.Namespace))
	defer factory.Shutdown()

	informer := factory.Core().V1().Pods().Informer()

	_, err = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			task.verifyPod(ctx, re, podMap, info.PodCount, obj, errs)
		},
		UpdateFunc: func(_, obj interface{}) {
			task.verifyPod(ctx, re, podMap, info.PodCount, obj, errs)
		},
	})
	if err != nil {
		return err
	}

	go informer.Run(ctx.Done())
	go func() {
		list, err := task.client.CoreV1().Pods(info.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			errs <- fmt.Errorf("%s: failed to list pods: %v", task.ID(), err)
			return
		}
		for i := range list.Items {
			if podMap.Size() == info.PodCount {
				break
			}
			task.verifyPod(ctx, re, podMap, info.PodCount, &list.Items[i], errs)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			return err
		}
	}
}

func (task *CheckPodTask) verifyLabels(ctx context.Context, pod *v1.Pod) error {
	if len(task.NodeLabels) == 0 || pod.Status.Phase != v1.PodRunning {
		return nil
	}

	node, err := task.client.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("%s: failed to get node '%s' for pod '%s': %v", task.ID(), pod.Spec.NodeName, pod.Name, err)
	}
	for key, val := range task.NodeLabels {
		if node.Labels[key] != val {
			return fmt.Errorf("%s: pod '%s' was scheduled on node '%s' without label '%s=%s'", task.ID(), pod.Name, pod.Spec.NodeName, key, val)
		}
		log.V(4).Infof("Verified pod %s for node %s with label %s:%s", pod.Name, pod.Spec.NodeName, key, val)
	}

	return nil
}

func (task *CheckPodTask) verifyPod(ctx context.Context, re []*regexp.Regexp, podMap *utils.SyncMap, count int, obj interface{}, errs chan error) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		errs <- fmt.Errorf("%s: unexpected object type %T, expected *v1.Pod", task.ID(), obj)
		return
	}

	for _, r := range re {
		if r.MatchString(pod.Name) {
			log.V(4).Infof("Matched pod %s", pod.Name)
			if _, ok := podMap.Get(pod.Name); ok {
				return
			}
			status := string(pod.Status.Phase)
			log.V(4).Infof("Informer event for pod %s with status %s", pod.Name, status)
			if status != task.Status {
				return
			}
			if err := task.verifyLabels(ctx, pod); err != nil {
				errs <- err
				return
			}
			if sz := podMap.Set(pod.Name, true); sz == count {
				log.Infof("Accounted for all pods")
				errs <- nil
				return
			}
		}
	}
}
