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
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	TaskSubmitObj   = "SubmitObj"
	TaskUpdateObj   = "UpdateObj"
	TaskCheckObj    = "CheckObj"
	TaskDeleteObj   = "DeleteObj"
	TaskCheckPod    = "CheckPod"
	TaskUpdateNodes = "UpdateNodes"
	TaskSleep       = "Sleep"
	TaskPause       = "Pause"
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

// ObjInfo contains object GVR and an optional list of derived pod names
type ObjInfo struct {
	Names     []string
	Namespace string
	GVR       schema.GroupVersionResource
	Pods      []string
}

// NewObjInfo creates new ObjInfo
func NewObjInfo(names []string, ns string, gvr schema.GroupVersionResource, pods ...string) *ObjInfo {
	return &ObjInfo{
		Names:     names,
		Namespace: ns,
		GVR:       gvr,
		Pods:      pods,
	}
}

// ObjSetter defines interface for setting ObjInfo
type ObjSetter interface {
	// SetObjInfo maps task ID to ObjInfo
	SetObjInfo(string, *ObjInfo) error
}

// ObjGetter defines interface for retrieving ObjInfo
type ObjGetter interface {
	// GetObjInfo returns ObjInfo for given task ID
	GetObjInfo(string) (*ObjInfo, error)
}

// CleanupInfo contains instructions on whether and how to clean up data after the test
type CleanupInfo struct {
	Enabled bool
	Timeout time.Duration
}
