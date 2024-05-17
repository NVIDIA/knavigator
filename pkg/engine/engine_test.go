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
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2/textlogger"

	"github.com/NVIDIA/knavigator/pkg/config"
)

var (
	errExec             = fmt.Errorf("exec error")
	errReset            = fmt.Errorf("reset error")
	testLogger          = textlogger.NewLogger(textlogger.NewConfig())
	testK8sClient       = &kubernetes.Clientset{}
	testDynamicClient   = &dynamic.DynamicClient{}
	testDiscoveryClient = &discovery.DiscoveryClient{}
)

type testEngine struct {
	execErr  error
	resetErr error
}

func (eng *testEngine) RunTask(context.Context, *config.Task) error {
	return eng.execErr
}

func (eng *testEngine) Reset(context.Context) error {
	return eng.resetErr
}

func (eng *testEngine) DeleteAllObjects(context.Context) {}

func TestRunEngine(t *testing.T) {
	testCases := []struct {
		name string
		eng  *testEngine
		err  error
	}{
		{
			name: "Case 1: exec error",
			eng:  &testEngine{execErr: errExec, resetErr: errReset},
			err:  errExec,
		},
		{
			name: "Case 2: reset error",
			eng:  &testEngine{resetErr: errReset},
			err:  errReset,
		},
		{
			name: "Case 3: no error",
			eng:  &testEngine{},
		},
	}

	ctx := context.Background()
	testCfg := &config.TaskConfig{
		Name:  "test",
		Tasks: []*config.Task{{ID: "task"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Run(ctx, tc.eng, testCfg)
			if tc.err != nil {
				require.Equal(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

type testRunnable struct {
	err error
}

func (r *testRunnable) ID() string {
	return ""
}

func (r *testRunnable) Exec(_ context.Context) error {
	return r.err
}

func TestExecRunnable(t *testing.T) {
	testCases := []struct {
		name string
		run  Runnable
		err  error
	}{
		{
			name: "Case 1: error",
			run:  &testRunnable{err: errExec},
			err:  errExec,
		},
		{
			name: "Case 2: no error",
			run:  &testRunnable{},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := execRunnable(ctx, testLogger, tc.run)
			if tc.err != nil {
				require.Equal(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
