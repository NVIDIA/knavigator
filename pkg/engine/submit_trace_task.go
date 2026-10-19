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
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/client-go/dynamic"
	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
)

// SubmitTraceTask generates a seeded synthetic workload and submits each job at its arrival time.
type SubmitTraceTask struct {
	BaseTask
	submitTraceTaskParams
	client   *dynamic.DynamicClient
	accessor ObjInfoAccessor
}

type submitTraceTaskParams struct {
	// RefTaskID: task ID of the RegisterObjTask used as the job template
	RefTaskID string `yaml:"refTaskId"`
	// Seed: random seed; the same seed and classes always give the same trace
	Seed uint64 `yaml:"seed"`
	// Speedup: divides arrival times and runtimes, so a trace of hours replays in minutes; default 1
	Speedup float64 `yaml:"speedup"`
	// TimeLimitFactor: scales the runtime into the time limit passed to the template; default 1
	TimeLimitFactor float64 `yaml:"timeLimitFactor"`
	// Params: template parameters shared by every job
	Params map[string]interface{} `yaml:"params"`
	// Classes: job populations, each with its own arrival rate and shape
	Classes []traceClass `yaml:"classes"`
}

type traceClass struct {
	Name string `yaml:"name"`
	Jobs int    `yaml:"jobs"`
	// ArrivalInterval: mean seconds between arrivals (Poisson)
	ArrivalInterval float64 `yaml:"arrivalInterval"`
	// Runtime: log-normal runtime in log-seconds, capped at Max seconds
	Runtime struct {
		Mu    float64 `yaml:"mu"`
		Sigma float64 `yaml:"sigma"`
		Max   float64 `yaml:"max"`
	} `yaml:"runtime"`
	// GPUs: per-pod GPU counts to pick from uniformly
	GPUs []int `yaml:"gpus"`
	// GangFraction: share of jobs that run as gangs, sized from GangSizes
	GangFraction float64 `yaml:"gangFraction"`
	GangSizes    []int   `yaml:"gangSizes"`
	// GangGPUs: per-pod GPUs for gang jobs; zero keeps the value picked from GPUs
	GangGPUs   int             `yaml:"gangGpus"`
	Priorities []int           `yaml:"priorities"`
	Accounts   []weightedValue `yaml:"accounts"`
}

type weightedValue struct {
	Name   string  `yaml:"name"`
	Weight float64 `yaml:"weight"`
}

type traceJob struct {
	ID       int
	Class    string
	Account  string
	Submit   float64
	Runtime  float64
	GPUs     int
	GangSize int
	Priority int
}

func newSubmitTraceTask(client *dynamic.DynamicClient, accessor ObjInfoAccessor, cfg *config.Task) (*SubmitTraceTask, error) {
	if client == nil {
		return nil, fmt.Errorf("%s/%s: DynamicClient is not set", cfg.Type, cfg.ID)
	}

	task := &SubmitTraceTask{
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

func (task *SubmitTraceTask) validate(params map[string]interface{}) error {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}
	if err = yaml.Unmarshal(data, &task.submitTraceTaskParams); err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}

	if len(task.RefTaskID) == 0 {
		return fmt.Errorf("%s: must specify refTaskId", task.ID())
	}
	if task.Speedup == 0 {
		task.Speedup = 1
	}
	if task.TimeLimitFactor == 0 {
		task.TimeLimitFactor = 1
	}
	if task.Speedup < 0 || task.TimeLimitFactor < 0 {
		return fmt.Errorf("%s: 'speedup' and 'timeLimitFactor' must be positive", task.ID())
	}
	if len(task.Classes) == 0 {
		return fmt.Errorf("%s: must specify classes", task.ID())
	}
	for _, c := range task.Classes {
		if c.Jobs <= 0 || c.ArrivalInterval <= 0 || c.Runtime.Max <= 0 || c.Runtime.Sigma < 0 || len(c.GPUs) == 0 {
			return fmt.Errorf("%s: class %q needs positive jobs, arrivalInterval and runtime.max, and gpus", task.ID(), c.Name)
		}
		if c.GangFraction > 0 && len(c.GangSizes) == 0 {
			return fmt.Errorf("%s: class %q has gangFraction but no gangSizes", task.ID(), c.Name)
		}
	}

	return nil
}

// generateTrace draws each class from its own random stream and merges them by arrival time.
func generateTrace(classes []traceClass, seed uint64) []traceJob {
	var jobs []traceJob
	for i, c := range classes {
		rng := rand.New(rand.NewPCG(seed, uint64(i)))
		now := 0.0
		for range c.Jobs {
			now += rng.ExpFloat64() * c.ArrivalInterval
			job := traceJob{
				Class:    c.Name,
				Submit:   now,
				Runtime:  math.Min(math.Exp(c.Runtime.Mu+c.Runtime.Sigma*rng.NormFloat64()), c.Runtime.Max),
				GPUs:     c.GPUs[rng.IntN(len(c.GPUs))],
				GangSize: 1,
			}
			if rng.Float64() < c.GangFraction {
				job.GangSize = c.GangSizes[rng.IntN(len(c.GangSizes))]
			}
			if job.GangSize > 1 && c.GangGPUs > 0 {
				job.GPUs = c.GangGPUs
			}
			if len(c.Priorities) != 0 {
				job.Priority = c.Priorities[rng.IntN(len(c.Priorities))]
			}
			job.Account = pickWeighted(rng, c.Accounts)
			jobs = append(jobs, job)
		}
	}

	sort.SliceStable(jobs, func(a, b int) bool { return jobs[a].Submit < jobs[b].Submit })
	for i := range jobs {
		jobs[i].ID = i + 1
	}
	return jobs
}

func pickWeighted(rng *rand.Rand, values []weightedValue) string {
	var total float64
	for _, v := range values {
		total += v.Weight
	}
	r := rng.Float64() * total
	for _, v := range values {
		if r -= v.Weight; r < 0 {
			return v.Name
		}
	}
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1].Name
}

// Exec implements Runnable interface
func (task *SubmitTraceTask) Exec(ctx context.Context) error {
	jobs := generateTrace(task.Classes, task.Seed)
	log.Infof("%s: submitting %d jobs at %gx speed", task.ID(), len(jobs), task.Speedup)

	start := time.Now()
	for i, job := range jobs {
		at := start.Add(time.Duration(job.Submit / task.Speedup * float64(time.Second)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(at)):
		}

		runtime := job.Runtime / task.Speedup
		limit := task.TimeLimitFactor * runtime
		params := map[string]interface{}{}
		for k, v := range task.Params {
			params[k] = v
		}
		params["jobId"] = job.ID
		params["class"] = job.Class
		params["account"] = job.Account
		params["gangSize"] = job.GangSize
		params["gpus"] = job.GPUs
		params["priority"] = job.Priority
		params["runtime"] = fmt.Sprintf("%.3fs", runtime)
		params["timeLimitSeconds"] = int(math.Ceil(limit))
		params["timeLimitMinutes"] = int(math.Ceil(limit / 60))

		// Each job is its own object so results can measure its wait from its own submission.
		submit := &SubmitObjTask{
			BaseTask:            BaseTask{taskType: TaskSubmitObj, taskID: fmt.Sprintf("%s-%d", task.taskID, job.ID)},
			submitObjTaskParams: submitObjTaskParams{RefTaskID: task.RefTaskID, Count: 1, Params: params},
			client:              task.client,
			accessor:            task.accessor,
		}
		if err := submit.Exec(ctx); err != nil {
			return err
		}
		if (i+1)%100 == 0 {
			log.Infof("%s: submitted %d/%d jobs", task.ID(), i+1, len(jobs))
		}
	}
	return nil
}
