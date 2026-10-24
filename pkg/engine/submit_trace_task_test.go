/*
 * Copyright (c) 2026, NVIDIA CORPORATION.  All rights reserved.
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
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/config"
)

func TestGenerateTrace(t *testing.T) {
	inference := traceClass{Name: "inference", Jobs: 300, ArrivalInterval: 6, GPUs: []int{1},
		Priorities: []int{100, 500}, Accounts: []weightedValue{{Name: "inference", Weight: 1}}}
	inference.Runtime.Mu, inference.Runtime.Sigma, inference.Runtime.Max = 5, 0.9, 1200
	training := traceClass{Name: "training", Jobs: 100, ArrivalInterval: 40, GPUs: []int{2, 4},
		GangFraction: 0.5, GangSizes: []int{2, 16}, GangGPUs: 8,
		Accounts: []weightedValue{{Name: "research", Weight: 0.6}, {Name: "trading", Weight: 0.4}}}
	training.Runtime.Mu, training.Runtime.Sigma, training.Runtime.Max = 8.4, 1.1, 14400
	classes := []traceClass{inference, training}

	jobs := generateTrace(classes, 7)
	require.Equal(t, jobs, generateTrace(classes, 7))
	require.NotEqual(t, jobs, generateTrace(classes, 8))
	require.Len(t, jobs, 400)

	gangs, accounts := 0, map[string]int{}
	for i, j := range jobs {
		require.Equal(t, i+1, j.ID)
		if i > 0 {
			require.GreaterOrEqual(t, j.Submit, jobs[i-1].Submit)
		}
		require.Positive(t, j.Runtime)
		if j.Class == "inference" {
			require.Equal(t, 1, j.GPUs)
			require.Equal(t, 1, j.GangSize)
			require.LessOrEqual(t, j.Runtime, 1200.0)
		}
		if j.GangSize > 1 {
			gangs++
			require.Equal(t, 8, j.GPUs)
		}
		accounts[j.Account]++
	}
	require.InDelta(t, 50, gangs, 20)
	require.Equal(t, 300, accounts["inference"])
	require.InDelta(t, 60, accounts["research"], 20)
}

func TestNewSubmitTraceTask(t *testing.T) {
	_, err := newSubmitTraceTask(nil, nil, &config.Task{Type: TaskSubmitTrace, ID: "trace"})
	require.Error(t, err)

	task := &SubmitTraceTask{BaseTask: BaseTask{taskType: TaskSubmitTrace, taskID: "trace"}}
	require.EqualError(t, task.validate(map[string]interface{}{"refTaskId": "job"}),
		"SubmitTrace/trace: must specify classes")
	require.EqualError(t, task.validate(map[string]interface{}{
		"refTaskId": "job",
		"classes":   []interface{}{map[string]interface{}{"name": "a", "jobs": 1, "arrivalInterval": 1, "gpus": []int{1}}},
	}), `SubmitTrace/trace: class "a" needs positive jobs, arrivalInterval and runtime.max, and gpus`)
	for _, size := range []int{0, -2} {
		require.EqualError(t, task.validate(map[string]interface{}{
			"refTaskId": "job",
			"classes": []interface{}{map[string]interface{}{"name": "a", "jobs": 1, "arrivalInterval": 1,
				"gpus": []int{1}, "runtime": map[string]interface{}{"max": 60},
				"gangFraction": 1, "gangSizes": []int{2, size}}},
		}), fmt.Sprintf(`SubmitTrace/trace: class "a" has gangSize %d; sizes must be positive`, size))
	}
	require.NoError(t, task.validate(map[string]interface{}{
		"refTaskId": "job",
		"classes": []interface{}{map[string]interface{}{"name": "a", "jobs": 1, "arrivalInterval": 1,
			"gpus": []int{1}, "runtime": map[string]interface{}{"max": 60}}},
	}))
	require.Equal(t, 1.0, task.Speedup)
}

func slowTraceTask(submit func(ctx context.Context, id int, params map[string]interface{}) error) *SubmitTraceTask {
	class := traceClass{Name: "a", Jobs: 3, ArrivalInterval: 0.004, GPUs: []int{1}}
	class.Runtime.Mu, class.Runtime.Max = 1, 10
	return &SubmitTraceTask{
		BaseTask:              BaseTask{taskType: TaskSubmitTrace, taskID: "trace"},
		submitTraceTaskParams: submitTraceTaskParams{Seed: 1, Speedup: 1, TimeLimitFactor: 1, Classes: []traceClass{class}},
		submitJob:             submit,
	}
}

func TestSubmitTraceIgnoresAPILatency(t *testing.T) {
	var submitted, inFlight, peak atomic.Int32
	task := slowTraceTask(func(_ context.Context, _ int, _ map[string]interface{}) error {
		n := inFlight.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(100 * time.Millisecond)
		inFlight.Add(-1)
		submitted.Add(1)
		return nil
	})

	start := time.Now()
	require.NoError(t, task.Exec(context.Background()))

	// Three 100ms submissions planned within a few milliseconds overlap instead of queueing.
	require.Equal(t, int32(3), submitted.Load())
	require.Greater(t, peak.Load(), int32(1))
	require.Less(t, time.Since(start), 250*time.Millisecond)
}

func TestSubmitTraceReportsSubmitError(t *testing.T) {
	boom := errors.New("boom")
	task := slowTraceTask(func(ctx context.Context, id int, _ map[string]interface{}) error {
		if id == 1 {
			return boom
		}
		<-ctx.Done()
		return ctx.Err()
	})

	require.ErrorIs(t, task.Exec(context.Background()), boom)
}
