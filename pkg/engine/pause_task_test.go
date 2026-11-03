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
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/config"
)

func TestPauseExec(t *testing.T) {
	eng, err := New(nil, nil, false)
	require.NoError(t, err)

	task, err := eng.GetTask(&config.Task{
		ID:   "pause",
		Type: TaskPause,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error)

	go func() {
		done <- task.Exec(ctx)
	}()

	select {
	case err := <-done:
		// user input is always simulated (scanner returns io.EOF in test mode)
		require.NoError(t, err)
	case <-ctx.Done():
		// we should always get user input before timeout
		require.NoError(t, ctx.Err())
	}
}

func TestPauseCanceled(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	pauseInput = r
	defer func() { pauseInput = os.Stdin }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- newPauseTask(&config.Task{ID: "pause"}).Exec(ctx) }()
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Pause stayed blocked on stdin after cancellation")
	}
}
