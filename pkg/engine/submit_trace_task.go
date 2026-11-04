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
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
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
	// AccountSet: generated accounts that share the jobs by a distribution; replaces Accounts
	AccountSet accountSet `yaml:"accountSet"`
	// Arrival: spreads the jobs over Duration seconds from Start instead of ArrivalInterval
	Arrival arrival `yaml:"arrival"`
}

// accountSet names Count accounts Prefix1..PrefixN, zero-padded, and splits a class's jobs over them.
type accountSet struct {
	Prefix string `yaml:"prefix"`
	Count  int    `yaml:"count"`
	// MinJobs: every account gets at least this many jobs before the rest are spread
	MinJobs      int `yaml:"minJobs"`
	Distribution struct {
		// Type: uniform (default), lognormal (mu, sigma over the account number) or gaussian
		// (mean, stddev as account numbers; default the middle account and a sixth of the count)
		Type   string  `yaml:"type"`
		Mu     float64 `yaml:"mu"`
		Sigma  float64 `yaml:"sigma"`
		Mean   float64 `yaml:"mean"`
		Stddev float64 `yaml:"stddev"`
	} `yaml:"distribution"`
}

// arrival places a class's jobs at random times over a window, all in seconds.
type arrival struct {
	// Shape: uniform (constant rate) or gaussian (ramps up, peaks, ramps down)
	Shape    string  `yaml:"shape"`
	Duration float64 `yaml:"duration"`
	// Mean, Stddev: gaussian only; default the middle of Duration and a sixth of it
	Mean   float64 `yaml:"mean"`
	Stddev float64 `yaml:"stddev"`
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
		if c.Jobs == 0 || c.Jobs < -1 || c.Runtime.Max <= 0 || c.Runtime.Sigma < 0 || len(c.GPUs) == 0 {
			return fmt.Errorf("%s: class %q needs jobs (-1 runs until stopped), positive runtime.max, and gpus", task.ID(), c.Name)
		}
		if c.ArrivalInterval < 0 || c.Start < 0 {
			return fmt.Errorf("%s: class %q has a negative arrivalInterval or start", task.ID(), c.Name)
		}
		if err := c.validateLoad(); err != nil {
			return fmt.Errorf("%s: class %q: %v", task.ID(), c.Name, err)
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

func (c traceClass) continuous() bool { return c.Jobs < 0 }

func (c traceClass) validateLoad() error {
	set, arr := c.AccountSet, c.Arrival
	if set.Count < 0 || set.MinJobs < 0 {
		return fmt.Errorf("accountSet count and minJobs must not be negative")
	}
	if set.Count > 0 {
		if len(c.Accounts) != 0 {
			return fmt.Errorf("use accounts or accountSet, not both")
		}
		if !c.continuous() && c.Jobs < set.Count*set.MinJobs {
			return fmt.Errorf("%d jobs cannot give %d accounts %d jobs each", c.Jobs, set.Count, set.MinJobs)
		}
		if c.continuous() && set.MinJobs > 0 {
			return fmt.Errorf("accountSet minJobs needs a fixed number of jobs")
		}
		switch t := set.Distribution.Type; t {
		case "", "uniform", "gaussian":
		case "lognormal":
			if set.Distribution.Sigma <= 0 {
				return fmt.Errorf("lognormal distribution needs a positive sigma")
			}
		default:
			return fmt.Errorf("unknown distribution %q", t)
		}
	}
	if arr.Shape == "" {
		return nil
	}
	if c.continuous() || c.Regular || c.ArrivalInterval != 0 {
		return fmt.Errorf("arrival needs a fixed number of jobs and no arrivalInterval or regular")
	}
	if arr.Shape != "uniform" && arr.Shape != "gaussian" {
		return fmt.Errorf("unknown arrival shape %q", arr.Shape)
	}
	if arr.Duration <= 0 || arr.Stddev < 0 {
		return fmt.Errorf("arrival needs a positive duration")
	}
	return nil
}

// accountWeights gives each account's share, by account number 1..Count.
func (s accountSet) weights() []float64 {
	w := make([]float64, s.Count)
	d := s.Distribution
	mean, std := d.Mean, d.Stddev
	if mean == 0 {
		mean = float64(s.Count+1) / 2
	}
	if std == 0 {
		std = float64(s.Count) / 6
	}
	for i := range w {
		k := float64(i + 1)
		switch d.Type {
		case "lognormal":
			z := (math.Log(k) - d.Mu) / d.Sigma
			w[i] = math.Exp(-z*z/2) / k
		case "gaussian":
			z := (k - mean) / std
			w[i] = math.Exp(-z * z / 2)
		default:
			w[i] = 1
		}
	}
	return w
}

func (s accountSet) names() []string {
	width := len(strconv.Itoa(s.Count))
	names := make([]string, s.Count)
	for i := range names {
		names[i] = fmt.Sprintf("%s%0*d", s.Prefix, width, i+1)
	}
	return names
}

// split shares jobs over the accounts: MinJobs each, the rest by weight. Rounding the running
// total keeps the counts summing to jobs.
func (s accountSet) split(jobs int) []int {
	w := s.weights()
	var total float64
	for _, v := range w {
		total += v
	}
	rest := float64(jobs - s.Count*s.MinJobs)
	counts := make([]int, s.Count)
	var cum float64
	prev := 0
	for i := range counts {
		cum += w[i]
		cur := int(math.Round(rest * cum / total))
		counts[i], prev = s.MinJobs+cur-prev, cur
	}
	return counts
}

// times draws n sorted arrival offsets over the window; gaussian draws outside it are redrawn.
func (a arrival) times(rng *rand.Rand, n int) []float64 {
	t := make([]float64, n)
	mean, std := a.Mean, a.Stddev
	if mean == 0 {
		mean = a.Duration / 2
	}
	if std == 0 {
		std = a.Duration / 6
	}
	for i := range t {
		if a.Shape == "uniform" {
			t[i] = rng.Float64() * a.Duration
			continue
		}
		// A narrow gaussian far from the window could redraw forever, so clamp after 100 tries.
		x := mean + std*rng.NormFloat64()
		for try := 0; (x < 0 || x > a.Duration) && try < 100; try++ {
			x = mean + std*rng.NormFloat64()
		}
		t[i] = math.Min(math.Max(x, 0), a.Duration)
	}
	sort.Float64s(t)
	return t
}

// classGen yields one class's jobs in arrival order, from its own random stream.
type classGen struct {
	c        traceClass
	rng      *rand.Rand
	k        int
	now      float64
	times    []float64 // set when Arrival is shaped
	accounts []string  // exact per-job accounts for a fixed-size accountSet, in shuffled order
	pool     []weightedValue
	head     *traceJob
}

func newClassGen(c traceClass, seed uint64, stream int) *classGen {
	g := &classGen{c: c, rng: rand.New(rand.NewPCG(seed, uint64(stream))), now: c.Start}
	if c.Arrival.Shape != "" {
		g.times = c.Arrival.times(g.rng, c.Jobs)
	}
	if set := c.AccountSet; set.Count > 0 {
		names := set.names()
		if c.continuous() {
			for i, w := range set.weights() {
				g.pool = append(g.pool, weightedValue{Name: names[i], Weight: w})
			}
		} else {
			for i, n := range set.split(c.Jobs) {
				for range n {
					g.accounts = append(g.accounts, names[i])
				}
			}
			g.rng.Shuffle(len(g.accounts), func(a, b int) { g.accounts[a], g.accounts[b] = g.accounts[b], g.accounts[a] })
		}
	} else {
		g.pool = c.Accounts
	}
	g.advance()
	return g
}

// advance draws the next job into head, or leaves it nil once the class is done.
func (g *classGen) advance() {
	c, k := g.c, g.k
	if !c.continuous() && k >= c.Jobs {
		g.head = nil
		return
	}
	g.k++
	switch {
	case g.times != nil:
		g.now = c.Start + g.times[k]
	case c.Regular:
		g.now = c.Start + float64(k)*c.ArrivalInterval
	default:
		g.now += g.rng.ExpFloat64() * c.ArrivalInterval
	}
	job := traceJob{
		Class:    c.Name,
		Submit:   g.now,
		Runtime:  math.Min(math.Exp(c.Runtime.Mu+c.Runtime.Sigma*g.rng.NormFloat64()), c.Runtime.Max),
		GPUs:     c.GPUs[g.rng.IntN(len(c.GPUs))],
		GangSize: 1,
	}
	if g.rng.Float64() < c.GangFraction {
		job.GangSize = c.GangSizes[g.rng.IntN(len(c.GangSizes))]
	}
	if job.GangSize > 1 && c.GangGPUs > 0 {
		job.GPUs = c.GangGPUs
	}
	if len(c.Priorities) != 0 {
		job.Priority = c.Priorities[g.rng.IntN(len(c.Priorities))]
	}
	if g.accounts != nil {
		job.Account = g.accounts[k]
	} else {
		job.Account = pickWeighted(g.rng, g.pool)
	}
	g.head = &job
}

// traceStream merges the classes by arrival time; ties go to the earlier class.
type traceStream struct {
	gens []*classGen
	id   int
}

func newTraceStream(classes []traceClass, seed uint64) *traceStream {
	s := &traceStream{}
	for i, c := range classes {
		s.gens = append(s.gens, newClassGen(c, seed, i))
	}
	return s
}

func (s *traceStream) next() (traceJob, bool) {
	var best *classGen
	for _, g := range s.gens {
		if g.head != nil && (best == nil || g.head.Submit < best.head.Submit) {
			best = g
		}
	}
	if best == nil {
		return traceJob{}, false
	}
	job := *best.head
	best.advance()
	s.id++
	job.ID = s.id
	return job, true
}

// generateTrace draws every job of fixed-size classes up front.
func generateTrace(classes []traceClass, seed uint64) []traceJob {
	var jobs []traceJob
	for s := newTraceStream(classes, seed); ; {
		job, ok := s.next()
		if !ok {
			return jobs
		}
		jobs = append(jobs, job)
	}
}

// traceDigest is a short fingerprint of a trace's jobs, so results can show whether two runs
// replayed the same trace. Times count to the millisecond, so a trace shifted to start at 0 keeps
// its digest when its times have at most three decimals.
func traceDigest(jobs []traceJob) string {
	h := sha256.New()
	for _, j := range jobs {
		fmt.Fprintf(h, "%d,%s,%s,%.3f,%.3f,%d,%d,%d\n", j.ID, j.Class, j.Account, j.Submit, j.Runtime, j.GPUs, j.GangSize, j.Priority)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// traceColumns is the header of a CSV trace.
var traceColumns = []string{"job_id", "account", "submit_time", "duration", "gpus", "gang_size", "priority"}

// readTrace reads jobs from a CSV trace, orders them by submit time with file order for ties, and
// shifts the times so the first job arrives at 0.
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
	first := jobs[0].Submit
	for i := range jobs {
		jobs[i].Submit -= first
	}
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
	next, jobs := task.source()
	continuous := jobs == nil
	// An open-ended trace has no fixed job list to fingerprint.
	digest := ""
	if continuous {
		log.Infof("%s: submitting jobs at %gx speed until stopped", task.ID(), task.Speedup)
	} else {
		digest = traceDigest(jobs)
		log.Infof("%s: submitting %d jobs of trace %s at %gx speed", task.ID(), len(jobs), digest, task.Speedup)
	}

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)

	// Submissions run in their own goroutines so API latency cannot delay later arrivals.
	start := time.Now()
	for i := 0; ; i++ {
		job, ok := next()
		if !ok {
			break
		}
		at := start.Add(time.Duration(job.Submit / task.Speedup * float64(time.Second)))
		select {
		case <-subCtx.Done():
		case <-time.After(time.Until(at)):
		}
		if subCtx.Err() != nil {
			break
		}

		params := task.jobParams(job)
		params["traceDigest"] = digest
		wg.Add(1)
		go func(id int, params map[string]interface{}) {
			defer wg.Done()
			if err := task.submitJob(subCtx, id, params); err != nil {
				once.Do(func() { firstErr = err; cancel() })
			}
		}(job.ID, params)
		if (i+1)%100 == 0 {
			log.Infof("%s: dispatched %d jobs", task.ID(), i+1)
		}
	}
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	// Stopping an open-ended run is how it ends, so the caller can still write results.
	if continuous {
		return nil
	}
	return ctx.Err()
}

// source returns the jobs in arrival order, and the whole trace unless a class never runs out.
func (task *SubmitTraceTask) source() (next func() (traceJob, bool), jobs []traceJob) {
	jobs = task.trace
	if jobs == nil {
		for _, c := range task.Classes {
			if c.continuous() {
				return newTraceStream(task.Classes, task.Seed).next, nil
			}
		}
		jobs = generateTrace(task.Classes, task.Seed)
	}
	i := 0
	return func() (traceJob, bool) {
		if i == len(jobs) {
			return traceJob{}, false
		}
		i++
		return jobs[i-1], true
	}, jobs
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
