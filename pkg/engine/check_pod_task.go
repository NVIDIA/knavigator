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
	"sort"
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

	client   kubernetes.Interface
	accessor ObjInfoAccessor
}

type checkPodTaskParams struct {
	RefTaskID  string            `yaml:"refTaskId"`
	Status     string            `yaml:"status"`
	NodeLabels map[string]string `yaml:"nodeLabels"`
	Timeout    time.Duration     `yaml:"timeout"`

	NodeLabelDomains *nodeLabelDomains `yaml:"nodeLabelDomains"`
}

// nodeLabelDomains limits the matched pods to at most Max distinct values of
// the node label Key, so Max 1 means all pods share one domain, such as a rack.
type nodeLabelDomains struct {
	Key string `yaml:"key"`
	Max int    `yaml:"max"`
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

	if len(task.Status) == 0 && len(task.NodeLabels) == 0 && task.NodeLabelDomains == nil {
		return fmt.Errorf("%s: missing parameters 'status', 'nodeLabels' and/or 'nodeLabelDomains'", task.ID())
	}

	if d := task.NodeLabelDomains; d != nil {
		if len(d.Key) == 0 {
			return fmt.Errorf("%s: parameter 'nodeLabelDomains.key' must be set", task.ID())
		}
		if d.Max < 1 {
			return fmt.Errorf("%s: parameter 'nodeLabelDomains.max' must be at least 1, got %d", task.ID(), d.Max)
		}
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
	podNodes := make(map[string]string)
	for i := range list.Items {
		pod := &list.Items[i]
		for _, r := range re {
			if r.MatchString(pod.Name) {
				log.V(4).Infof("Matched pod %s", pod.Name)
				count++

				status := string(pod.Status.Phase)
				if !task.statusMatches(status) {
					return fmt.Errorf("%s: pod %s, status %s, expected %s", task.ID(), pod.Name, status, task.Status)
				}

				if err := task.verifyLabels(ctx, pod); err != nil {
					return err
				}

				if task.NodeLabelDomains != nil {
					if len(pod.Spec.NodeName) == 0 {
						return fmt.Errorf("%s: pod '%s' has no node, so nodeLabelDomains cannot place it", task.ID(), pod.Name)
					}
					podNodes[pod.Name] = pod.Spec.NodeName
				}
			}
		}
	}

	if count != info.PodCount {
		return fmt.Errorf("%s: verified %d pods, expected %d", task.ID(), count, info.PodCount)
	}

	return task.verifyDomains(ctx, podNodes)
}

// statusMatches reports whether a pod phase satisfies 'status'. Without 'status',
// a nodeLabelDomains check accepts every phase: it judges where pods run, not how.
func (task *CheckPodTask) statusMatches(phase string) bool {
	if len(task.Status) == 0 && task.NodeLabelDomains != nil {
		return true
	}
	return phase == task.Status
}

// verifyDomains fails when the nodes of the given pods (pod name to node name)
// take more than nodeLabelDomains.max distinct values of nodeLabelDomains.key.
// A node without the label fails the check rather than count as a value or be skipped.
func (task *CheckPodTask) verifyDomains(ctx context.Context, podNodes map[string]string) error {
	d := task.NodeLabelDomains
	if d == nil {
		return nil
	}

	pods := make([]string, 0, len(podNodes))
	for pod := range podNodes {
		pods = append(pods, pod)
	}
	sort.Strings(pods)

	nodeValues := make(map[string]string)
	domains := make(map[string]bool)
	for _, pod := range pods {
		nodeName := podNodes[pod]
		val, ok := nodeValues[nodeName]
		if !ok {
			node, err := task.client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("%s: failed to get node '%s' for pod '%s': %v", task.ID(), nodeName, pod, err)
			}
			if val, ok = node.Labels[d.Key]; !ok {
				return fmt.Errorf("%s: node '%s' of pod '%s' has no label '%s'", task.ID(), nodeName, pod, d.Key)
			}
			nodeValues[nodeName] = val
		}
		domains[val] = true
	}

	if len(domains) > d.Max {
		values := make([]string, 0, len(domains))
		for val := range domains {
			values = append(values, val)
		}
		sort.Strings(values)
		return fmt.Errorf("%s: pods on %d distinct values of %s, at most %d allowed: %v", task.ID(), len(domains), d.Key, d.Max, values)
	}

	log.V(4).Infof("Verified %d pods on %d distinct values of %s", len(pods), len(domains), d.Key)
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
			if !task.statusMatches(status) {
				return
			}
			if err := task.verifyLabels(ctx, pod); err != nil {
				errs <- err
				return
			}
			// A pod without a node has no domain yet: wait for it to be placed.
			if task.NodeLabelDomains != nil && len(pod.Spec.NodeName) == 0 {
				return
			}
			if sz := podMap.Set(pod.Name, pod.Spec.NodeName); sz == count {
				log.Infof("Accounted for all pods")
				errs <- task.verifyDomains(ctx, podNodes(podMap))
				return
			}
		}
	}
}

// podNodes returns the pod name to node name pairs that verifyPod stored in podMap.
func podNodes(podMap *utils.SyncMap) map[string]string {
	nodes := make(map[string]string)
	for _, key := range podMap.Keys() {
		if val, ok := podMap.Get(key); ok {
			nodes[key.(string)], _ = val.(string)
		}
	}
	return nodes
}
