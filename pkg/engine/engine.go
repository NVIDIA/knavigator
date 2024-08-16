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
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type Engine interface {
	RunTask(context.Context, *config.Task) error
	Reset(context.Context) error
	DeleteAllObjects(context.Context)
}

type Eng struct {
	mutex           sync.Mutex
	k8sClient       *kubernetes.Clientset
	dynamicClient   *dynamic.DynamicClient
	discoveryClient *discovery.DiscoveryClient
	objTypeMap      map[string]*RegisterObjParams
	objInfoMap      map[string]*ObjInfo
	cleanup         *CleanupInfo
}

func New(config *rest.Config, cleanupInfo *CleanupInfo, sim ...bool) (*Eng, error) {
	eng := &Eng{
		objTypeMap: make(map[string]*RegisterObjParams),
		objInfoMap: make(map[string]*ObjInfo),
		cleanup:    cleanupInfo,
	}

	if len(sim) == 0 { // len(sim) != 0 in unit tests
		var err error
		if eng.k8sClient, err = kubernetes.NewForConfig(config); err != nil {
			return nil, err
		}
		if eng.dynamicClient, err = dynamic.NewForConfig(config); err != nil {
			return nil, err
		}
		if eng.discoveryClient, err = discovery.NewDiscoveryClientForConfig(config); err != nil {
			return nil, err
		}
	} else if sim[0] {
		eng.k8sClient = &kubernetes.Clientset{}
		eng.dynamicClient = &dynamic.DynamicClient{}
		eng.discoveryClient = &discovery.DiscoveryClient{}
	}

	return eng, nil
}

func Run(ctx context.Context, eng Engine, workflow *config.Workflow) error {
	var errExec error
	for _, cfg := range workflow.Tasks {
		if errExec = eng.RunTask(ctx, cfg); errExec != nil {
			break
		}
	}

	errReset := eng.Reset(ctx)

	if errExec != nil {
		return errExec
	}

	return errReset
}

func (eng *Eng) RunTask(ctx context.Context, cfg *config.Task) error {
	runnable, err := eng.GetTask(cfg)
	if err != nil {
		return err
	}

	return execRunnable(ctx, runnable)
}

// GetTask initializes and validates task
func (eng *Eng) GetTask(cfg *config.Task) (Runnable, error) {
	eng.mutex.Lock()
	defer eng.mutex.Unlock()

	log.Infof("Creating task %s/%s", cfg.Type, cfg.ID)
	switch cfg.Type {
	case TaskRegisterObj:
		return newRegisterObjTask(eng.discoveryClient, eng, cfg)

	case TaskConfigure:
		return newConfigureTask(eng.k8sClient, cfg)

	case TaskSubmitObj:
		task, err := newSubmitObjTask(eng.dynamicClient, eng, cfg)
		if err != nil {
			return nil, err
		}
		if _, ok := eng.objTypeMap[task.RefTaskID]; !ok {
			return nil, fmt.Errorf("%s: unreferenced task ID %s", task.ID(), task.RefTaskID)
		}
		return task, nil

	case TaskUpdateObj:
		task, err := newUpdateObjTask(eng.dynamicClient, eng, cfg)
		if err != nil {
			return nil, err
		}
		if _, ok := eng.objInfoMap[task.RefTaskID]; !ok {
			return nil, fmt.Errorf("%s: unreferenced task ID %s", task.ID(), task.RefTaskID)
		}
		return task, nil

	case TaskCheckObj:
		task, err := newCheckObjTask(eng.dynamicClient, eng, cfg)
		if err != nil {
			return nil, err
		}
		if _, ok := eng.objInfoMap[task.RefTaskID]; !ok {
			return nil, fmt.Errorf("%s: unreferenced task ID %s", task.ID(), task.RefTaskID)
		}
		return task, nil

	case TaskDeleteObj:
		task, err := newDeleteObjTask(eng.dynamicClient, eng, cfg)
		if err != nil {
			return nil, err
		}
		if _, ok := eng.objInfoMap[task.RefTaskID]; !ok {
			return nil, fmt.Errorf("%s: unreferenced task ID %s", task.ID(), task.RefTaskID)
		}
		return task, nil

	case TaskUpdateNodes:
		return newUpdateNodesTask(eng.k8sClient, cfg)

	case TaskCheckPod:
		task, err := newCheckPodTask(eng.k8sClient, eng, cfg)
		if err != nil {
			return nil, err
		}
		if _, ok := eng.objInfoMap[task.RefTaskID]; !ok {
			return nil, fmt.Errorf("%s: unreferenced task ID %s", task.ID(), task.RefTaskID)
		}
		return task, nil

	case TaskCheckConfigmap:
		task, err := newCheckConfigmapTask(eng.k8sClient, cfg)
		if err != nil {
			return nil, err
		}
		return task, nil

	case TaskSleep:
		return newSleepTask(cfg)

	case TaskPause:
		return newPauseTask(cfg), nil

	default:
		return nil, fmt.Errorf("unsupported task type %q", cfg.Type)
	}
}

// SetObjType implements ObjSetter interface and maps object type to RegisterObjParams
func (eng *Eng) SetObjType(taskID string, params *RegisterObjParams) error {
	eng.mutex.Lock()
	defer eng.mutex.Unlock()

	if _, ok := eng.objTypeMap[taskID]; ok {
		return fmt.Errorf("SetObjType: duplicate task ID %s", taskID)
	}

	eng.objTypeMap[taskID] = params

	log.V(4).Infof("Registering object for taskID %s", taskID)

	return nil
}

// GetObjType implements ObjGetter interface returns RegisterObjParams for given object type
func (eng *Eng) GetObjType(objType string) (*RegisterObjParams, error) {
	eng.mutex.Lock()
	defer eng.mutex.Unlock()

	info, ok := eng.objTypeMap[objType]
	if !ok {
		return nil, fmt.Errorf("GetObjType: missing object type %s", objType)
	}

	log.V(4).Infof("Getting object type %s", objType)

	return info, nil
}

// SetObjInfo implements ObjSetter interface and maps task ID to the corresponding ObjInfo
func (eng *Eng) SetObjInfo(taskID string, info *ObjInfo) error {
	eng.mutex.Lock()
	defer eng.mutex.Unlock()

	if _, ok := eng.objInfoMap[taskID]; ok {
		return fmt.Errorf("SetObjInfo: duplicate taskID %s", taskID)
	}

	eng.objInfoMap[taskID] = info

	log.V(4).Infof("Setting task info for task ID %s", taskID)

	return nil
}

// GetObjInfo implements ObjGetter interface returns ObjInfo for given task ID
func (eng *Eng) GetObjInfo(taskID string) (*ObjInfo, error) {
	eng.mutex.Lock()
	defer eng.mutex.Unlock()

	info, ok := eng.objInfoMap[taskID]
	if !ok {
		return nil, fmt.Errorf("GetObjInfo: missing task ID %s", taskID)
	}

	log.V(4).Infof("Getting task info for taskID %s", taskID)

	return info, nil
}

func execRunnable(ctx context.Context, r Runnable) error {
	id := r.ID()
	log.Infof("Starting task %s", id)
	start := time.Now()
	if err := r.Exec(ctx); err != nil {
		log.Errorf("Task %s failed", id)
		return err
	}
	log.Infof("Task %s completed in %s", id, time.Since(start).String())
	return nil
}

// Reset re-initializes engine and deletes the remaining objects
func (eng *Eng) Reset(ctx context.Context) error {
	log.Infof("Reset Engine")

	if eng.cleanup == nil || !eng.cleanup.Enabled {
		return nil
	}

	log.Infof("Cleaning up objects")
	ctx, cancel := context.WithTimeout(ctx, eng.cleanup.Timeout)
	defer cancel()

	stop := make(chan struct{})

	go func() {
		eng.DeleteAllObjects(ctx)
		stop <- struct{}{}
	}()

	select {
	case <-stop:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DeleteAllObjects deletes all objects
func (eng *Eng) DeleteAllObjects(ctx context.Context) {
	deletePolicy := metav1.DeletePropagationBackground
	deletions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	for _, objInfo := range eng.objInfoMap {
		ns := objInfo.Namespace
		for _, name := range objInfo.Names {
			for i := range objInfo.GVR {
				err := eng.dynamicClient.Resource(objInfo.GVR[i]).Namespace(ns).Delete(ctx, name, deletions)
				if err != nil {
					log.Infof("Warning: cannot delete object %s: %v", name, err)
				}
			}
		}
	}

	log.Infof("Deleted all objects")
}
