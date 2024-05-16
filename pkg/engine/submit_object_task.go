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
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

type SubmitObjTask struct {
	BaseTask
	submitObjTaskParams
	client   *dynamic.DynamicClient
	accessor ObjInfoAccessor
}

type submitObjTaskParams struct {
	// RefTaskID: task ID of the corresponding RegisterObjTask
	RefTaskID string `yaml:"refTaskId"`
	// Count: number of objects to submit; default 1.
	Count int `yaml:"count"`
	// Params: a map of key:value pairs to be used when executing object and name templates.
	Params map[string]interface{} `yaml:"params"`
}

type objectMeta struct {
	Name        string             `json:"name" yaml:"name"`
	Namespace   string             `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels      map[string]*string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations map[string]*string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

type GenericObject struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata objectMeta  `json:"metadata" yaml:"metadata"`
	Spec     interface{} `json:"spec" yaml:"spec"`
}

// newSubmitObjTask initializes and returns SubmitObjTask
func newSubmitObjTask(log logr.Logger, client *dynamic.DynamicClient, accessor ObjInfoAccessor, cfg *config.Task) (*SubmitObjTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: DynamicClient is not set", cfg.Type, cfg.ID)
	}

	task := &SubmitObjTask{
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

// validate initializes and validates parameters for SubmitObjTask.
func (task *SubmitObjTask) validate(params map[string]interface{}) error {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}
	if err = yaml.Unmarshal(data, &task.submitObjTaskParams); err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}

	if len(task.RefTaskID) == 0 {
		return fmt.Errorf("%s: must specify refTaskId", task.ID())
	}

	if task.Count == 0 {
		task.Count = 1 // default
	} else if task.Count < 0 {
		return fmt.Errorf("%s: 'count' must be a positive number", task.ID())
	}

	return nil
}

// Exec implements Runnable interface
func (task *SubmitObjTask) Exec(ctx context.Context) error {
	regObjParams, err := task.accessor.GetObjType(task.RefTaskID)
	if err != nil {
		return fmt.Errorf("%s: failed to get object type: %v", task.ID(), err)
	}

	objs, podCount, podRegexp, err := task.getGenericObjects(regObjParams)
	if err != nil {
		return err
	}

	for _, obj := range objs {
		crd := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": obj.APIVersion,
				"kind":       obj.Kind,
				"metadata":   obj.Metadata,
				"spec":       obj.Spec,
			},
		}

		if _, err := task.client.Resource(regObjParams.gvr).Namespace(obj.Metadata.Namespace).Create(ctx, crd, metav1.CreateOptions{}); err != nil {
			return err
		}
	}

	return task.accessor.SetObjInfo(task.taskID,
		NewObjInfo([]string{objs[0].Metadata.Name}, objs[0].Metadata.Namespace, regObjParams.gvr, podCount, podRegexp...))
}

func (task *SubmitObjTask) getGenericObjects(regObjParams *RegisterObjParams) ([]GenericObject, int, []string, error) {
	names, err := utils.GenerateNames(regObjParams.NameFormat, task.Count, task.Params)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("%s: failed to generate object names: %v", task.ID(), err)
	}

	objs := make([]GenericObject, task.Count)
	podRegexp := []string{}

	for i := 0; i < task.Count; i++ {
		task.Params["_NAME_"] = names[i]

		data, err := utils.ExecTemplate(regObjParams.objTpl, task.Params)
		if err != nil {
			return nil, 0, nil, err
		}

		if err = yaml.Unmarshal(data, &objs[i]); err != nil {
			return nil, 0, nil, err
		}

		if regObjParams.podNameTpl != nil {
			data, err = utils.ExecTemplate(regObjParams.podNameTpl, task.Params)
			if err != nil {
				return nil, 0, nil, err
			}
			re := strings.Trim(strings.TrimSpace(string(data)), "\"")
			podRegexp = append(podRegexp, re)
		}
	}

	var podCount int
	if regObjParams.podCountTpl != nil {
		data, err := utils.ExecTemplate(regObjParams.podCountTpl, task.Params)
		if err != nil {
			return nil, 0, nil, err
		}
		str := string(data)
		podCount, err = strconv.Atoi(str)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("%s: failed to convert pod count %s to int: %v", task.ID(), str, err)
		}
		podCount *= task.Count
	}
	task.log.V(4).Info("Generating object specs", "podCount", podCount, "podRegexp", podRegexp)

	return objs, podCount, podRegexp, nil
}

func (obj *GenericObject) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var o struct {
		TypeMeta `yaml:",inline"`
		Metadata objectMeta             `yaml:"metadata"`
		Spec     map[string]interface{} `yaml:"spec"`
	}

	err := unmarshal(&o)
	if err != nil {
		return err
	}

	obj.TypeMeta = o.TypeMeta
	obj.Metadata = o.Metadata
	obj.Spec = convertMap(o.Spec)
	return nil
}

func convertMap(obj interface{}) interface{} {
	switch v := obj.(type) {
	case map[interface{}]interface{}:
		converted := make(map[string]interface{})
		for key, val := range v {
			converted[fmt.Sprintf("%v", key)] = convertMap(val)
		}
		return converted
	case []interface{}:
		for i, val := range v {
			v[i] = convertMap(val)
		}
	case map[string]interface{}:
		for key, val := range v {
			v[key] = convertMap(val)
		}
	}
	return obj
}
