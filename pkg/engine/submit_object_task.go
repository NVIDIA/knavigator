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
	"text/template"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

type SubmitObjTask struct {
	BaseTask
	submitObjTaskParams
	client *dynamic.DynamicClient
	setter ObjSetter

	// derived
	obj []GenericObject
}

type submitObjTaskParams struct {
	// Count: number of objects to submit; default 1.
	Count int `json:"count"`
	// GRV: Group/Version/Resource of the object.
	GRV groupVersionResource `json:"grv"`
	// Template: path to the object template; see examples in resources/templates/
	Template string `json:"template"`
	// NameFormat: a Go-template parameter for generating unique object names.
	// It utilizes the '_ENUM_' keyword for an incrementing counter and
	// adds the '_NAME_' key to the Overrides map with the templated value.
	// Example: "job{{._ENUM_}}"
	NameFormat string `json:"nameformat"`
	// Overrides: a map of key:value pairs to be used when executing object and name templates.
	Overrides map[string]interface{} `json:"overrides"`
	// Pods: an optional parameter for specifying the naming format of pods spawned by the object(s).
	Pods utils.NameSelector `json:"pods,omitempty"`
}

type groupVersionResource struct {
	Group    string `json:"group" yaml:"group"`
	Version  string `json:"version" yaml:"version"`
	Resource string `json:"resource" yaml:"resource"`
}

type typeMeta struct {
	Kind       string `json:"kind" yaml:"kind"`
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
}

type objectMeta struct {
	Name        string             `json:"name" yaml:"name"`
	Namespace   string             `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels      map[string]*string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations map[string]*string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

type GenericObject struct {
	typeMeta `json:",inline" yaml:",inline"`
	Metadata objectMeta  `json:"metadata" yaml:"metadata"`
	Spec     interface{} `json:"spec" yaml:"spec"`
}

// newSubmitObjTask initializes and returns SubmitObjTask
func newSubmitObjTask(log logr.Logger, client *dynamic.DynamicClient, setter ObjSetter, cfg *config.Task) (*SubmitObjTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: DynamicClient is not set", cfg.Type, cfg.ID)
	}

	task := &SubmitObjTask{
		BaseTask: BaseTask{
			log:      log,
			taskType: cfg.Type,
			taskID:   cfg.ID,
		},
		client: client,
		setter: setter,
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

	if task.Count == 0 {
		task.Count = 1 // default
	} else if task.Count < 0 {
		return fmt.Errorf("%s: 'count' must be a positive number", task.ID())
	}

	if len(task.Template) == 0 {
		return fmt.Errorf("%s: 'template' must be a filepath", task.ID())
	}

	tpl, err := template.ParseFiles(task.Template)
	if err != nil {
		return fmt.Errorf("%s: failed to parse template %s: %v", task.ID(), task.Template, err)
	}

	if len(task.NameFormat) == 0 {
		if task.Count > 1 {
			return fmt.Errorf("%s: must specify name format for multiple object submissions", task.ID())
		}
	}

	task.obj = make([]GenericObject, task.Count)
	names, err := utils.GenerateNames(task.NameFormat, task.Count, task.Overrides)
	if err != nil {
		return fmt.Errorf("%s: failed to generate object names: %v", task.ID(), err)
	}

	task.Pods.Init()
	if task.Pods.List != nil {
		if task.Pods.List.Params == nil {
			task.Pods.List.Params = make(map[string]interface{})
		}
	}
	if task.Pods.Range != nil {
		if task.Pods.Range.Params == nil {
			task.Pods.Range.Params = make(map[string]interface{})
		}
	}

	for i := 0; i < task.Count; i++ {
		task.Overrides["_NAME_"] = names[i]

		data, err = utils.ExecTemplate(tpl, task.Overrides)
		if err != nil {
			return err
		}

		if err = yaml.Unmarshal(data, &task.obj[i]); err != nil {
			return err
		}

		if task.Pods.List != nil {
			task.Pods.List.Params["_NAME_"] = task.obj[i].Metadata.Name
		}
		if task.Pods.Range != nil {
			task.Pods.Range.Params["_NAME_"] = task.obj[i].Metadata.Name
		}
		if err = task.Pods.Finalize(); err != nil {
			return err
		}
	}

	if pods := task.Pods.Names(); len(pods) != 0 {
		task.log.V(4).Info("Expected pods", "names", pods)
	}
	return nil
}

// Exec implements Runnable interface
func (task *SubmitObjTask) Exec(ctx context.Context) error {
	gvr := schema.GroupVersionResource{
		Group:    task.GRV.Group,
		Version:  task.GRV.Version,
		Resource: task.GRV.Resource,
	}
	for _, obj := range task.obj {
		crd := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": obj.APIVersion,
				"kind":       obj.Kind,
				"metadata":   obj.Metadata,
				"spec":       obj.Spec,
			},
		}

		if _, err := task.client.Resource(gvr).Namespace(obj.Metadata.Namespace).Create(ctx, crd, metav1.CreateOptions{}); err != nil {
			return err
		}
	}

	task.setter.SetObjInfo(task.taskID,
		NewObjInfo([]string{task.obj[0].Metadata.Name}, task.obj[0].Metadata.Namespace, gvr, task.Pods.Names()...))

	return nil
}

func (obj *GenericObject) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var o struct {
		typeMeta `yaml:",inline"`
		Metadata objectMeta             `yaml:"metadata"`
		Spec     map[string]interface{} `yaml:"spec"`
	}

	err := unmarshal(&o)
	if err != nil {
		return err
	}

	obj.typeMeta = o.typeMeta
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
