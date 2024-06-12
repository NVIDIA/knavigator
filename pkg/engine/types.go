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
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	TaskConfigure   = "Configure"
	TaskRegisterObj = "RegisterObj"
	TaskSubmitObj   = "SubmitObj"
	TaskUpdateObj   = "UpdateObj"
	TaskCheckObj    = "CheckObj"
	TaskDeleteObj   = "DeleteObj"
	TaskCheckPod    = "CheckPod"
	TaskUpdateNodes = "UpdateNodes"
	TaskSleep       = "Sleep"
	TaskPause       = "Pause"

	OpCreate = "create"
	OpDelete = "delete"

	DefaultCleanupTimeout = 5 * time.Minute
)

type Runnable interface {
	ID() string
	Exec(context.Context) error
}

type BaseTask struct {
	taskType string
	taskID   string
	log      logr.Logger
}

// ID implements Runnable interface
func (t *BaseTask) ID() string {
	return fmt.Sprintf("%s/%s", t.taskType, t.taskID)
}

type StateParams struct {
	RefTaskID string                 `yaml:"refTaskId"`
	State     map[string]interface{} `yaml:"state"`
	Timeout   time.Duration          `yaml:"timeout"`
}

type TypeMeta struct {
	Kind       string `json:"kind" yaml:"kind"`
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
}

type RegisterObjParams struct {
	// Template: path to the object template; see examples in resources/templates/
	Template string `yaml:"template"`
	// NameFormat: a Go-template parameter for generating unique object names.
	// It utilizes the '_ENUM_' keyword for an incrementing counter and
	// adds the '_NAME_' key to the parameter map with the templated value.
	// Example: "job{{._ENUM_}}"
	NameFormat string `yaml:"nameFormat"`
	// PodNameFormat: an optional Go-template parameter for specifying regexp for the naming format
	// of pods spawned by the object(s). It utilizes the '_NAME_' keyword for the object name.
	// PodNameFormat should be specified when a user intends to use 'CheckPod' task.
	// Example: "{{._NAME_}}-\d+-\S+"
	PodNameFormat string `yaml:"podNameFormat,omitempty"`
	// PodCount: an optional Go-template parameter for specifying number of spawned pods per object.
	// It can contain a numerical value or refer to the template parameter.
	// PodCount should be specified when a user intends to use 'CheckPod' task.
	// Example: "2" or "{{.replicas}}"
	PodCount string `yaml:"podCount,omitempty"`

	// derived
	gvr         schema.GroupVersionResource
	objTpl      *template.Template
	podNameTpl  *template.Template
	podCountTpl *template.Template
}

// ObjInfo contains object GVR and an optional list of derived pod names
type ObjInfo struct {
	Names     []string
	Namespace string
	GVR       schema.GroupVersionResource
	PodCount  int
	PodRegexp []string
}

// NewObjInfo creates new ObjInfo
func NewObjInfo(names []string, ns string, gvr schema.GroupVersionResource, podCount int, podRegexp ...string) *ObjInfo {
	return &ObjInfo{
		Names:     names,
		Namespace: ns,
		GVR:       gvr,
		PodCount:  podCount,
		PodRegexp: podRegexp,
	}
}

// ObjInfoAccessor defines interface for getting and setting object info
type ObjInfoAccessor interface {
	// SetObjType maps object type to RegisterObjParams
	SetObjType(string, *RegisterObjParams) error
	// GetObjType returns RegisterObjParams for given object type, where object type is formatted as "<resource>.<group>"
	GetObjType(string) (*RegisterObjParams, error)
	// SetObjInfo maps task ID to ObjInfo
	SetObjInfo(string, *ObjInfo) error
	// GetObjInfo returns ObjInfo for given task ID
	GetObjInfo(string) (*ObjInfo, error)
}

// CleanupInfo contains instructions on whether and how to clean up data after the test
type CleanupInfo struct {
	Enabled bool
	Timeout time.Duration
}
