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
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"text/template"

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
func newRegisterObjTask(client *discovery.DiscoveryClient, accessor ObjInfoAccessor, cfg *config.Task) (*RegisterObjTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: DiscoveryClient is not set", cfg.Type, cfg.ID)
	}

	task := &RegisterObjTask{
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

	tplStr := string(tplData)
	var ver, kind string
	scanner := bufio.NewScanner(strings.NewReader(tplStr))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "apiVersion:") {
			ver = strings.TrimSpace(line[11:])
		}
		if strings.HasPrefix(line, "kind:") {
			kind = strings.TrimSpace(line[5:])
		}
		if len(ver) != 0 && len(kind) != 0 {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading string:", err)
	}
	if len(ver) == 0 {
		return fmt.Errorf("%s: failed to fetch 'apiVersion' from template %s: %d", task.ID(), task.Template, len(ver))
	}
	if len(kind) == 0 {
		return fmt.Errorf("%s: failed to fetch 'kind' from template %s: %d", task.ID(), task.Template, len(kind))
	}

	task.gvk = schema.FromAPIVersionAndKind(ver, kind)

	task.objTpl, err = template.New("object").Parse(tplStr)
	if err != nil {
		return fmt.Errorf("%s: failed to parse template %s: %v", task.ID(), task.Template, err)
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
