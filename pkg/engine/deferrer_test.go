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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type testExecutor struct {
	tasks []string
}

func (exec *testExecutor) RunTask(_ context.Context, cfg *config.Task) error {
	exec.tasks = append(exec.tasks, cfg.ID)
	return nil
}

func TestDeferrer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	exec := &testExecutor{tasks: []string{}}
	deferrer := NewDereffer(testLogger, exec)
	deferrer.Start(ctx)

	deferrer.Inc(6)
	deferrer.AddTask(&config.Task{ID: "t3"}, 3*time.Second)
	deferrer.AddTask(&config.Task{ID: "t1"}, 1*time.Second)
	deferrer.AddTask(&config.Task{ID: "t5"}, 5*time.Second)
	deferrer.AddTask(&config.Task{ID: "t4"}, 4*time.Second)
	deferrer.AddTask(&config.Task{ID: "t2"}, 2*time.Second)
	deferrer.AddTask(&config.Task{ID: "t6"}, 6*time.Second)

	err := deferrer.Wait(ctx, 8*time.Second)
	require.NoError(t, err)
	require.Equal(t, []string{"t1", "t2", "t3", "t4", "t5", "t6"}, exec.tasks)
}
