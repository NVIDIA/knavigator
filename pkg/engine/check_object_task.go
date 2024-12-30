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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

// CheckObjTask represents a task that checks object state and status.
type CheckObjTask struct {
	ObjStateTask
}

// newCheckObjTask initializes and returns CheckObjTask
func newCheckObjTask(client *dynamic.DynamicClient, accessor ObjInfoAccessor, cfg *config.Task) (*CheckObjTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: DynamicClient is not set", cfg.Type, cfg.ID)
	}

	task := &CheckObjTask{
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
func (task *CheckObjTask) Exec(ctx context.Context) error {
	info, err := task.accessor.GetObjInfo(task.RefTaskID)
	if err != nil {
		return err
	}

	nameMap := utils.NewSyncMap()
	for _, name := range info.Names {
		nameMap.Set(name, true)
	}

	// Check once and return if timeout is not set
	if task.Timeout == 0 {
		return task.checkStates(ctx, info, nameMap)
	}

	// Keep checking until timeout
	ctx, cancel := context.WithTimeout(ctx, task.Timeout)
	defer cancel()

	// TODO: add TweakListOptionsFunc for the CR
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(task.client, 0, info.Namespace, nil)
	informer := factory.ForResource(info.GVR[task.Index]).Informer()

	done := make(chan struct{})
	defer close(done)

	_, err = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			resource := obj.(*unstructured.Unstructured)
			log.V(4).Infof("Informer added %s %s", info.GVR[task.Index].Resource, resource.GetName())
			task.checkStateAsync(ctx, resource.GetName(), info, nameMap, done)
		},
		UpdateFunc: func(_, obj interface{}) {
			resource := obj.(*unstructured.Unstructured)
			log.V(4).Infof("Informer updated %s %s", info.GVR[task.Index].Resource, resource.GetName())
			task.checkStateAsync(ctx, resource.GetName(), info, nameMap, done)
		},
	})
	if err != nil {
		return err
	}

	stopCh := make(chan struct{})
	go informer.Run(stopCh)

	// check the objects synchronously, then use informer
	if err = task.checkStates(ctx, info, nameMap); err != nil {
		log.V(4).Infof("Wait for completion with informers")
		select {
		case <-ctx.Done():
			log.Errorf("Validation failed for %s %v, err: %v", info.GVR[task.Index].Resource, nameMap.Keys(), err)
			err = ctx.Err()
		case <-done:
			log.Infof("Validation passed for %s", info.GVR[task.Index].Resource)
			err = nil
		}
	}
	close(stopCh)

	return err
}

func (task *CheckObjTask) checkStates(ctx context.Context, info *ObjInfo, nameMap *utils.SyncMap) error {
	for _, name := range info.Names {
		if err := task.checkState(ctx, name, info, nameMap); err != nil {
			log.V(4).Info(err.Error())
		}
	}

	if invalid := nameMap.Keys(); len(invalid) != 0 {
		return fmt.Errorf("%s: failed to validate %s %v", task.ID(), info.GVR[task.Index].Resource, nameMap.Keys())
	}

	log.Infof("Validation passed for %s", info.GVR[task.Index].Resource)
	return nil
}

func (task *CheckObjTask) checkStateAsync(ctx context.Context, name string, info *ObjInfo, nameMap *utils.SyncMap, done chan struct{}) {
	if err := task.checkState(ctx, name, info, nameMap); err != nil {
		log.V(4).Info(err.Error())
		return
	}

	if nameMap.Size() == 0 {
		done <- struct{}{}
	}
}

// checkState validates state conformance and removes object name from the map if succeeded
func (task *CheckObjTask) checkState(ctx context.Context, name string, info *ObjInfo, nameMap *utils.SyncMap) error {
	gvr := info.GVR[task.Index]
	cr, err := task.client.Resource(gvr).Namespace(info.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("%s: failed to get %s %s: %v", task.ID(), gvr.Resource, name, err)
	}
	if !utils.IsSubset(cr.Object, task.State) {
		return fmt.Errorf("%s: state mismatch in %s %s", task.ID(), gvr.Resource, name)
	}

	nameMap.Delete(name)
	return nil
}
