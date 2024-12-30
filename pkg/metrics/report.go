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

package metrics

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	log "k8s.io/klog/v2"
)

type resourceList struct {
	requests corev1.ResourceList
	limits   corev1.ResourceList
}

func ReportResourceUsage(ctx context.Context, client *kubernetes.Clientset, namespace string, resources, nodeLabels []string) {
	start := time.Now()
	nodeResources, err := getNodeResourceMap(ctx, client, namespace)
	if err != nil {
		log.Infof("ERROR: %v", err)
		return
	}

	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Infof("ERROR: failed to list the nodes: %v", err)
		return
	}

	empty := &resourceList{
		requests: make(corev1.ResourceList),
		limits:   make(corev1.ResourceList),
	}

	for _, node := range nodes.Items {
		nodeLabelValues := make([]string, len(nodeLabels))
		for i, name := range nodeLabels {
			nodeLabelValues[i] = node.Labels[name]
		}

		list, ok := nodeResources[node.Name]
		if !ok {
			list = empty
		}

		var val float64
		for _, resource := range resources {
			labelValues := append([]string{node.Name, resource}, nodeLabelValues...)
			// get resource requests
			if v, ok := list.requests[corev1.ResourceName(resource)]; ok {
				val = v.AsApproximateFloat64()
			} else {
				val = 0
			}
			nodeResourceRequests.WithLabelValues(labelValues...).Set(val)
			// get resource usage in percents
			if v, ok := node.Status.Allocatable[corev1.ResourceName(resource)]; ok {
				if allocatable := v.AsApproximateFloat64(); allocatable > 0 {
					occ := val / allocatable

					log.V(4).InfoS("metrics", "node", node.Name, "resource", resource, "allocatable", allocatable, "requests", val, "occupancy", occ)
					nodeResourceOccupancy.WithLabelValues(labelValues...).Set(occ * 100.0)
				}
			}
			// get resource limits
			if v, ok := list.limits[corev1.ResourceName(resource)]; ok {
				val = v.AsApproximateFloat64()
			} else {
				val = 0
			}
			nodeResourceLimits.WithLabelValues(labelValues...).Set(val)
		}
	}
	log.V(4).Infof("Reporting cycle took %s", time.Since(start).String())
}

func getNodeResourceMap(ctx context.Context, kubeClient *kubernetes.Clientset, namespace string) (map[string]*resourceList, error) {
	pods, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list the pods: %v", err)
	}
	log.V(4).Infof("Found %d pods in %q namespace", len(pods.Items), namespace)

	nodeResources := make(map[string]*resourceList)
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		list, ok := nodeResources[pod.Spec.NodeName]
		if !ok {
			list = &resourceList{
				requests: make(corev1.ResourceList),
				limits:   make(corev1.ResourceList),
			}
			nodeResources[pod.Spec.NodeName] = list
		}
		for _, container := range pod.Spec.Containers {
			addResourceList(list.requests, container.Resources.Requests)
			addResourceList(list.limits, container.Resources.Limits)
		}
	}

	log.V(4).Infof("Created resource map for %d nodes", len(nodeResources))
	return nodeResources, nil
}

func addResourceList(total, addition corev1.ResourceList) {
	for resourceName, quantity := range addition {
		if curr, found := total[resourceName]; found {
			curr.Add(quantity)
			total[resourceName] = curr
		} else {
			total[resourceName] = quantity.DeepCopy()
		}
	}
}
