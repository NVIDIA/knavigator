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
	"os"
	"path/filepath"
	"sync"
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

func TestGenerateTraceBursts(t *testing.T) {
	class := func(name string, jobs int, interval, start float64, regular bool) traceClass {
		c := traceClass{Name: name, Jobs: jobs, ArrivalInterval: interval, Start: start, Regular: regular, GPUs: []int{8}}
		c.Runtime.Mu, c.Runtime.Max = 8, 3600
		return c
	}
	trickle := class("trickle", 50, 60, 0, false)
	poisson := generateTrace([]traceClass{trickle}, 3)

	jobs := generateTrace([]traceClass{
		trickle,
		class("pile", 4, 0, 3600, false),
		class("spaced", 3, 7200, 1800, true),
	}, 3)

	submits := map[string][]float64{}
	for _, j := range jobs {
		submits[j.Class] = append(submits[j.Class], j.Submit)
	}
	require.Equal(t, []float64{3600, 3600, 3600, 3600}, submits["pile"], "interval 0 lands every job at start")
	require.Equal(t, []float64{1800, 9000, 16200}, submits["spaced"], "regular arrivals start at start, evenly spaced")
	for i, j := range poisson {
		require.Equal(t, j.Submit, submits["trickle"][i], "new classes leave an existing class's draws alone")
	}
}

func TestNewSubmitTraceTask(t *testing.T) {
	_, err := newSubmitTraceTask(nil, nil, &config.Task{Type: TaskSubmitTrace, ID: "trace"})
	require.Error(t, err)

	task := &SubmitTraceTask{BaseTask: BaseTask{taskType: TaskSubmitTrace, taskID: "trace"}}
	require.EqualError(t, task.validate(map[string]interface{}{"refTaskId": "job"}),
		"SubmitTrace/trace: must specify classes or file")
	require.EqualError(t, task.validate(map[string]interface{}{
		"refTaskId": "job",
		"classes":   []interface{}{map[string]interface{}{"name": "a", "jobs": 1, "arrivalInterval": 1, "gpus": []int{1}}},
	}), `SubmitTrace/trace: class "a" needs positive jobs and runtime.max, and gpus`)
	require.EqualError(t, task.validate(map[string]interface{}{
		"refTaskId": "job",
		"classes": []interface{}{map[string]interface{}{"name": "a", "jobs": 1, "start": -1,
			"gpus": []int{1}, "runtime": map[string]interface{}{"max": 60}}},
	}), `SubmitTrace/trace: class "a" has a negative arrivalInterval or start`)
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

func writeTrace(t *testing.T, content string) string {
	path := filepath.Join(t.TempDir(), "trace.csv")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

const traceHeader = "job_id,account,submit_time,duration,gpus,gang_size,priority\n"

func TestReadTrace(t *testing.T) {
	// The lab writes times with three decimals. Jobs 7 and 3 arrive together and keep file order.
	path := writeTrace(t, "\ufeff"+traceHeader+
		"5,research,30.500,600.000,8,4,0\n"+
		"7,trading,0.000,90.250,1,1,500\n"+
		"3,infra,0.000,45.000,2,1,-10\n")
	jobs, err := readTrace(path)
	require.NoError(t, err)
	require.Equal(t, []traceJob{
		{ID: 7, Account: "trading", Submit: 0, Runtime: 90.25, GPUs: 1, GangSize: 1, Priority: 500},
		{ID: 3, Account: "infra", Submit: 0, Runtime: 45, GPUs: 2, GangSize: 1, Priority: -10},
		{ID: 5, Account: "research", Submit: 30.5, Runtime: 600, GPUs: 8, GangSize: 4, Priority: 0},
	}, jobs)
}

func TestReadTraceErrors(t *testing.T) {
	testCases := []struct {
		name    string
		content string
		err     string
	}{
		{
			name:    "Case 1: wrong header",
			content: "id,account,submit,duration,gpus,gang_size,priority\n1,a,0,1,1,1,0\n",
			err:     "header must be job_id,account,submit_time,duration,gpus,gang_size,priority, got id,account,submit,duration,gpus,gang_size,priority",
		},
		{
			name:    "Case 2: no jobs",
			content: traceHeader,
			err:     "trace has no jobs",
		},
		{
			name:    "Case 3: missing column",
			content: traceHeader + "1,a,0,1,1,1\n",
			err:     "record on line 2: wrong number of fields",
		},
		{
			name:    "Case 4: job_id not positive",
			content: traceHeader + "0,a,0,1,1,1,0\n",
			err:     `line 2: job_id "0" must be a positive integer`,
		},
		{
			name:    "Case 5: duplicate job_id",
			content: traceHeader + "1,a,0,1,1,1,0\n2,a,1,1,1,1,0\n1,b,2,1,1,1,0\n",
			err:     "line 4: job_id 1 is already used on line 2",
		},
		{
			name:    "Case 6: negative submit_time",
			content: traceHeader + "1,a,-1,1,1,1,0\n",
			err:     `line 2: submit_time "-1" must be a number of seconds, zero or more`,
		},
		{
			name:    "Case 7: zero duration",
			content: traceHeader + "1,a,0,0,1,1,0\n",
			err:     `line 2: duration "0" must be a positive number of seconds`,
		},
		{
			name:    "Case 8: duration NaN",
			content: traceHeader + "1,a,0,NaN,1,1,0\n",
			err:     `line 2: duration "NaN" must be a positive number of seconds`,
		},
		{
			name:    "Case 9: fractional gpus",
			content: traceHeader + "1,a,0,1,1.5,1,0\n",
			err:     `line 2: gpus "1.5" must be a positive integer`,
		},
		{
			name:    "Case 9a: no gpus",
			content: traceHeader + "1,a,0,1,0,1,0\n",
			err:     `line 2: gpus "0" must be a positive integer`,
		},
		{
			name:    "Case 10: zero gang_size",
			content: traceHeader + "1,a,0,1,1,0,0\n",
			err:     `line 2: gang_size "0" must be a positive integer`,
		},
		{
			name:    "Case 11: priority not a number",
			content: traceHeader + "1,a,0,1,1,1,high\n",
			err:     `line 2: priority "high" must be an integer`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTrace(t, tc.content)
			_, err := readTrace(path)
			require.EqualError(t, err, path+": "+tc.err)
		})
	}

	_, err := readTrace(filepath.Join(t.TempDir(), "missing.csv"))
	require.ErrorContains(t, err, "failed to open trace: ")
}

func TestSubmitTraceFile(t *testing.T) {
	path := writeTrace(t, traceHeader+"2,research,0.200,120,8,2,100\n1,trading,0,60,1,1,0\n")
	newTask := func() *SubmitTraceTask {
		return &SubmitTraceTask{BaseTask: BaseTask{taskType: TaskSubmitTrace, taskID: "trace"}}
	}

	require.EqualError(t, newTask().validate(map[string]interface{}{
		"refTaskId": "job",
		"file":      path,
		"classes":   []interface{}{map[string]interface{}{"name": "a"}},
	}), "SubmitTrace/trace: specify either classes or file, not both")
	require.EqualError(t, newTask().validate(map[string]interface{}{"refTaskId": "job", "file": path + ".bad"}),
		fmt.Sprintf("SubmitTrace/trace: failed to open trace: open %s.bad: no such file or directory", path))

	task := newTask()
	require.NoError(t, task.validate(map[string]interface{}{"refTaskId": "job", "file": path, "speedup": 2}))
	var (
		mu    sync.Mutex
		order []int
		got   = map[int]map[string]interface{}{}
	)
	task.submitJob = func(_ context.Context, id int, params map[string]interface{}) error {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, id)
		got[id] = params
		return nil
	}
	require.NoError(t, task.Exec(context.Background()))

	// Jobs keep their trace IDs and replay in submit order; -speedup halves the runtimes.
	require.Equal(t, []int{1, 2}, order)
	require.Equal(t, "30.000s", got[1]["runtime"])
	require.Equal(t, "research", got[2]["account"])
	require.Equal(t, 8, got[2]["gpus"])
	require.Equal(t, 2, got[2]["gangSize"])
	require.Equal(t, 100, got[2]["priority"])
	require.Equal(t, "60.000s", got[2]["runtime"])
}
