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
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/client-go/dynamic"
	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
)

// SubmitTraceTask submits each job of a workload at its arrival time. The workload is either
// generated from seeded job classes or read from a CSV trace file.
type SubmitTraceTask struct {
	BaseTask
	submitTraceTaskParams
	client   *dynamic.DynamicClient
	accessor ObjInfoAccessor
	// trace holds the jobs read from File; nil when they are generated from Classes.
	trace []traceJob
	// submitJob creates one job; tests replace it.
	submitJob func(ctx context.Context, id int, params map[string]interface{}) error
}

type submitTraceTaskParams struct {
	// RefTaskID: task ID of the RegisterObjTask used as the job template
	RefTaskID string `yaml:"refTaskId"`
	// Seed: random seed; the same seed and classes always give the same trace
	Seed uint64 `yaml:"seed"`
	// Speedup: divides arrival times and runtimes; main sets it from -speedup so results scale to match
	Speedup float64 `yaml:"speedup"`
	// TimeLimitFactor: scales the runtime into the time limit passed to the template; default 1
	TimeLimitFactor float64 `yaml:"timeLimitFactor"`
	// Params: template parameters shared by every job
	Params map[string]interface{} `yaml:"params"`
	// Classes: job populations, each with its own arrival rate and shape
	Classes []traceClass `yaml:"classes"`
	// File: CSV trace to replay instead of generating jobs from Classes
	File string `yaml:"file"`
}

type traceClass struct {
	Name string `yaml:"name"`
	Jobs int    `yaml:"jobs"`
	// ArrivalInterval: mean seconds between arrivals (Poisson); 0 submits every job at Start
	ArrivalInterval float64 `yaml:"arrivalInterval"`
	// Start: seconds before the class's arrivals begin
	Start float64 `yaml:"start"`
	// Regular: arrivals exactly ArrivalInterval apart instead of Poisson, the first at Start
	Regular bool `yaml:"regular"`
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
	task.submitJob = task.submitObject

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
	switch {
	case len(task.File) != 0 && len(task.Classes) != 0:
		return fmt.Errorf("%s: specify either classes or file, not both", task.ID())
	case len(task.File) != 0:
		if task.trace, err = readTrace(task.File); err != nil {
			return fmt.Errorf("%s: %v", task.ID(), err)
		}
		return nil
	case len(task.Classes) == 0:
		return fmt.Errorf("%s: must specify classes or file", task.ID())
	}
	for _, c := range task.Classes {
		if c.Jobs <= 0 || c.Runtime.Max <= 0 || c.Runtime.Sigma < 0 || len(c.GPUs) == 0 {
			return fmt.Errorf("%s: class %q needs positive jobs and runtime.max, and gpus", task.ID(), c.Name)
		}
		if c.ArrivalInterval < 0 || c.Start < 0 {
			return fmt.Errorf("%s: class %q has a negative arrivalInterval or start", task.ID(), c.Name)
		}
		if c.GangFraction > 0 && len(c.GangSizes) == 0 {
			return fmt.Errorf("%s: class %q has gangFraction but no gangSizes", task.ID(), c.Name)
		}
		for _, size := range c.GangSizes {
			if size <= 0 {
				return fmt.Errorf("%s: class %q has gangSize %d; sizes must be positive", task.ID(), c.Name, size)
			}
		}
	}

	return nil
}

// generateTrace draws each class from its own random stream and merges them by arrival time.
func generateTrace(classes []traceClass, seed uint64) []traceJob {
	var jobs []traceJob
	for i, c := range classes {
		rng := rand.New(rand.NewPCG(seed, uint64(i)))
		now := c.Start
		for k := range c.Jobs {
			if c.Regular {
				now = c.Start + float64(k)*c.ArrivalInterval
			} else {
				now += rng.ExpFloat64() * c.ArrivalInterval
			}
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

// traceColumns is the header of a CSV trace.
var traceColumns = []string{"job_id", "account", "submit_time", "duration", "gpus", "gang_size", "priority"}

// readTrace reads jobs from a CSV trace and orders them by submit time, keeping file order for ties.
func readTrace(path string) ([]traceJob, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open trace: %v", err)
	}
	defer f.Close() //nolint:errcheck

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: failed to read header: %v", path, err)
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	if !slices.Equal(header, traceColumns) {
		return nil, fmt.Errorf("%s: header must be %s, got %s", path, strings.Join(traceColumns, ","), strings.Join(header, ","))
	}

	var jobs []traceJob
	lines := map[int]int{}
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %v", path, err)
		}
		line, _ := r.FieldPos(0)
		job, err := parseTraceJob(record)
		if err != nil {
			return nil, fmt.Errorf("%s: line %d: %v", path, line, err)
		}
		if prev, ok := lines[job.ID]; ok {
			return nil, fmt.Errorf("%s: line %d: job_id %d is already used on line %d", path, line, job.ID, prev)
		}
		lines[job.ID] = line
		jobs = append(jobs, job)
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("%s: trace has no jobs", path)
	}

	sort.SliceStable(jobs, func(a, b int) bool { return jobs[a].Submit < jobs[b].Submit })
	return jobs, nil
}

func parseTraceJob(record []string) (traceJob, error) {
	job := traceJob{Account: record[1]}
	var err error
	if job.ID, err = strconv.Atoi(record[0]); err != nil || job.ID <= 0 {
		return job, fmt.Errorf("job_id %q must be a positive integer", record[0])
	}
	if job.Submit, err = strconv.ParseFloat(record[2], 64); err != nil || !(job.Submit >= 0) || math.IsInf(job.Submit, 0) {
		return job, fmt.Errorf("submit_time %q must be a number of seconds, zero or more", record[2])
	}
	if job.Runtime, err = strconv.ParseFloat(record[3], 64); err != nil || !(job.Runtime > 0) || math.IsInf(job.Runtime, 0) {
		return job, fmt.Errorf("duration %q must be a positive number of seconds", record[3])
	}
	// -results only records pods that request GPUs, so it would wait out its timeout for a job without them.
	if job.GPUs, err = strconv.Atoi(record[4]); err != nil || job.GPUs <= 0 {
		return job, fmt.Errorf("gpus %q must be a positive integer", record[4])
	}
	if job.GangSize, err = strconv.Atoi(record[5]); err != nil || job.GangSize <= 0 {
		return job, fmt.Errorf("gang_size %q must be a positive integer", record[5])
	}
	if job.Priority, err = strconv.Atoi(record[6]); err != nil {
		return job, fmt.Errorf("priority %q must be an integer", record[6])
	}
	return job, nil
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
	jobs := task.trace
	if jobs == nil {
		jobs = generateTrace(task.Classes, task.Seed)
	}
	log.Infof("%s: submitting %d jobs at %gx speed", task.ID(), len(jobs), task.Speedup)

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)

	// Submissions run in their own goroutines so API latency cannot delay later arrivals.
	start := time.Now()
	for i, job := range jobs {
		at := start.Add(time.Duration(job.Submit / task.Speedup * float64(time.Second)))
		select {
		case <-subCtx.Done():
		case <-time.After(time.Until(at)):
		}
		if subCtx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(id int, params map[string]interface{}) {
			defer wg.Done()
			if err := task.submitJob(subCtx, id, params); err != nil {
				once.Do(func() { firstErr = err; cancel() })
			}
		}(job.ID, task.jobParams(job))
		if (i+1)%100 == 0 {
			log.Infof("%s: dispatched %d/%d jobs", task.ID(), i+1, len(jobs))
		}
	}
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func (task *SubmitTraceTask) jobParams(job traceJob) map[string]interface{} {
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
	return params
}

// submitObject is the default submitJob. Each job is its own object so results can measure its
// wait from its own submission.
func (task *SubmitTraceTask) submitObject(ctx context.Context, id int, params map[string]interface{}) error {
	return (&SubmitObjTask{
		BaseTask:            BaseTask{taskType: TaskSubmitObj, taskID: fmt.Sprintf("%s-%d", task.taskID, id)},
		submitObjTaskParams: submitObjTaskParams{RefTaskID: task.RefTaskID, Count: 1, Params: params},
		client:              task.client,
		accessor:            task.accessor,
	}).Exec(ctx)
}
