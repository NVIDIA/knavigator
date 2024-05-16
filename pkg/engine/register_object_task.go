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
	"os"
	"text/template"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type RegisterObjTask struct {
	BaseTask
	RegisterObjParams

	client   *discovery.DiscoveryClient
	accessor ObjInfoAccessor

	gvk schema.GroupVersionKind
}

// newRegisterObjTask initializes and returns RegisterObjTask
func newRegisterObjTask(log logr.Logger, client *discovery.DiscoveryClient, accessor ObjInfoAccessor, cfg *config.Task) (*RegisterObjTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: DiscoveryClient is not set", cfg.Type, cfg.ID)
	}

	task := &RegisterObjTask{
		BaseTask: BaseTask{
			log:      log,
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

// validate initializes and validates parameters for RegisterObjTask.
func (task *RegisterObjTask) validate(params map[string]interface{}) error {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}
	if err = yaml.Unmarshal(data, &task.RegisterObjParams); err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}

	if len(task.Template) == 0 {
		return fmt.Errorf("%s: must specify template", task.ID())
	}

	tplData, err := os.ReadFile(task.Template)
	if err != nil {
		return fmt.Errorf("%s: failed to read %s: %v", task.ID(), task.Template, err)
	}

	var typeMeta TypeMeta
	err = yaml.Unmarshal(tplData, &typeMeta)
	if err != nil {
		return fmt.Errorf("%s: failed to parse template %s: %v", task.ID(), task.Template, err)
	}

	task.gvk = schema.FromAPIVersionAndKind(typeMeta.APIVersion, typeMeta.Kind)

	task.objTpl, err = template.ParseFiles(task.Template)
	if err != nil {
		return fmt.Errorf("%s: failed to parse template %s: %v", task.ID(), task.Template, err)
	}

	if len(task.NameFormat) == 0 {
		return fmt.Errorf("%s: must specify nameFormat", task.ID())
	}

	if len(task.PodNameFormat) != 0 {
		if task.podNameTpl, err = template.New("podname").Parse(task.PodNameFormat); err != nil {
			return fmt.Errorf("%s: failed to parse podname template: %v", task.ID(), err)
		}
	}

	if len(task.PodCount) != 0 {
		if task.podNameTpl == nil {
			return fmt.Errorf("%s: must define podNameFormat with podCount", task.ID())
		}
		if task.podCountTpl, err = template.New("podcount").Parse(task.PodCount); err != nil {
			return fmt.Errorf("%s: failed to parse podcount template: %v", task.ID(), err)
		}
	} else if task.podNameTpl != nil {
		return fmt.Errorf("%s: must define podCount with podNameFormat", task.ID())
	}

	return nil
}

// Exec implements Runnable interface
func (task *RegisterObjTask) Exec(ctx context.Context) error {
	switch task.gvk.String() {
	case "batch/v1, Kind=Job":
		task.gvr = schema.GroupVersionResource{
			Group:    task.gvk.Group,
			Version:  task.gvk.Version,
			Resource: "jobs",
		}
	default:
		if err := task.getGVR(); err != nil {
			return err
		}
	}

	return task.accessor.SetObjType(task.taskID, &task.RegisterObjParams)
}

func (task *RegisterObjTask) getGVR() error {
	apiResourceList, err := task.client.ServerPreferredResources()
	if err != nil {
		return fmt.Errorf("%s: failed to retrieve API resources: %v", task.ID(), err)
	}

	for _, list := range apiResourceList {
		for _, r := range list.APIResources {
			if r.Group == task.gvk.Group && r.Kind == task.gvk.Kind {
				task.gvr = schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Name}
				return nil
			}
		}
	}

	return fmt.Errorf("%s: failed to find resource for %s", task.ID(), task.gvk.String())
}
