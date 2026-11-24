/*
 * SPDX-FileCopyrightText: Copyright (c) 2024 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/engine"
	"github.com/NVIDIA/knavigator/pkg/results"
	"github.com/NVIDIA/knavigator/pkg/server"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

type Args struct {
	kubeCfg      config.KubeConfig
	workflow     string
	vars         config.Vars
	port         int
	cleanupInfo  engine.CleanupInfo
	results      string
	resultsWait  time.Duration
	speedup      float64
	pushURL      string
	pushRun      string
	pushEvery    time.Duration
	resultsStall time.Duration
	bsldFloor    time.Duration
	aggregate    string
	restartNS    string
	leftovers    bool
	rackLabel    string
	baseline     string
	profile      string
	topologyKeys string
}

func mainInternal() error {
	args := Args{vars: config.Vars{}}
	flag.StringVar(&args.kubeCfg.KubeConfigPath, "kubeconfig", "", "kubeconfig file path")
	flag.StringVar(&args.kubeCfg.KubeCtx, "kubectx", "", "kube context")
	flag.Float64Var(&args.kubeCfg.QPS, "kube-api-qps", 500, "Maximum QPS to use while talking with Kubernetes API")
	flag.IntVar(&args.kubeCfg.Burst, "kube-api-burst", 500, "Maximum burst for throttle while talking with Kubernetes API")
	flag.BoolVar(&args.cleanupInfo.Enabled, "cleanup", false, "delete objects")
	flag.DurationVar(&args.cleanupInfo.Timeout, "cleanup.timeout", engine.DefaultCleanupTimeout, "time limit for cleanup")
	flag.StringVar(&args.workflow, "workflow", "", "comma-separated list of workflow config files and dirs (mutually exclusive with the 'port' flag)")
	flag.Var(args.vars, "var", "name=value for ${name} or ${name:-default} in workflow files; repeatable (requires 'workflow')")
	flag.IntVar(&args.port, "port", 0, "listening port (mutually exclusive with the 'workflow' flag)")
	flag.StringVar(&args.results, "results", "", "directory for run results; waits for submitted pods to finish (requires 'workflow')")
	flag.DurationVar(&args.resultsWait, "results.timeout", time.Hour, "time limit for pods to finish before writing results")
	flag.DurationVar(&args.bsldFloor, "results.bsld-floor", results.DefaultBSLDFloor, "runtime floor for bounded slowdown, in simulated time")

	flag.StringVar(&args.pushURL, "results.pushgateway", "", "Prometheus Pushgateway URL; pushes live run metrics and the summary (requires 'results')")
	flag.StringVar(&args.pushRun, "results.run", "", "run label on pushed metrics (default: start time)")
	flag.DurationVar(&args.pushEvery, "results.push-interval", 10*time.Second, "how often to push live metrics")
	flag.Float64Var(&args.speedup, "speedup", 1, "divide Sleep timeouts and SubmitObj 'ttl' params by this factor and speed up SubmitTrace by it; results report simulated time (requires 'workflow')")
	flag.DurationVar(&args.resultsStall, "results.stall", 0, "stop waiting when no pod is placed or finishes for this long, in real time; 0 disables")
	flag.StringVar(&args.aggregate, "aggregate", "", "glob of -results directories from repeated runs; prints each metric's mean, spread and 95% interval as JSON, then exits")
	flag.StringVar(&args.restartNS, "results.restart-namespaces", "", "comma-separated namespaces whose container restarts fail the run, besides the control-plane pods")
	flag.BoolVar(&args.leftovers, "results.allow-leftovers", false, "start even while GPU pods from before the run are active: log them, record the GPUs of those bound to a node as harness.leftover_gpus and subtract them from capacity for the whole run; utilization reads high if they finish during it, and low if an unbound one is placed during it")
	flag.StringVar(&args.rackLabel, "results.rack-label", "", "node label that names each node's rack; fills gpu_fragmentation_rack in the results (requires 'results')")
	flag.StringVar(&args.profile, "results.profile", "", fmt.Sprintf("scheduler setup the run measures, as a lowercase DNS label; names the run's entry in manifest.json, which reads %q without it (requires 'results')", results.UnknownProfile))
	flag.StringVar(&args.topologyKeys, "results.topology-labels", "", "comma-separated node label keys of topology levels, such as example.com/block; manifest.json counts the distinct values of each, and of 'results.rack-label', over the GPU nodes (requires 'results')")
	flag.StringVar(&args.baseline, "aggregate.baseline", "", "glob of -results directories to compare against; adds each metric's difference from them and its 95% interval (requires 'aggregate')")

	log.InitFlags(nil)
	flag.Parse()

	if len(args.aggregate) != 0 {
		return aggregate(os.Stdout, args.aggregate, args.baseline)
	}

	if err := validate(&args); err != nil {
		flag.Usage()
		return err
	}

	restConfig, err := utils.GetK8sConfig(&args.kubeCfg)
	if err != nil {
		return err
	}

	// Point helm at the same cluster, so runs against different contexts can share a host.
	if len(args.kubeCfg.KubeConfigPath) != 0 {
		if err = os.Setenv("KUBECONFIG", args.kubeCfg.KubeConfigPath); err != nil {
			return err
		}
	}
	if len(args.kubeCfg.KubeCtx) != 0 {
		if err = os.Setenv("HELM_KUBECONTEXT", args.kubeCfg.KubeCtx); err != nil {
			return err
		}
	}

	eng, err := engine.New(restConfig, &args.cleanupInfo)
	if err != nil {
		return err
	}

	if args.port > 0 {
		return server.New(eng, args.port).Run()
	}

	workflows, err := config.NewFromPaths(args.workflow, args.vars)
	if err != nil {
		return err
	}

	if err := speedUp(workflows, args.speedup); err != nil {
		return err
	}

	// Stop on the first signal so the run is marked aborted; a second signal kills as before.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	var rec *results.Recorder
	if len(args.results) != 0 {
		client, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return err
		}
		opts := results.HarnessOptions{
			RestartNamespaces: splitList(args.restartNS),
			AllowLeftovers:    args.leftovers,
		}
		if rec, err = results.Start(ctx, client, opts); err != nil {
			return err
		}
		rec.SetRackLabel(args.rackLabel)
		rec.SetRunInfo(results.RunInfo{
			Profile:        args.profile,
			TopologyLabels: splitList(args.topologyKeys),
			Workflows:      workflowsOf(workflows),
			Vars:           args.vars,
		})
		if len(args.pushURL) != 0 {
			if len(args.pushRun) == 0 {
				args.pushRun = time.Now().UTC().Format("20060102T150405")
			}
			rec.PushTo(ctx, args.pushURL, args.pushRun, args.pushEvery, func() []results.Job { return submittedJobs(eng) }, args.speedup)
		}
	}

	var runErr error
	for _, workflow := range workflows {
		log.Infof("Starting workflow %s", workflow.Name)
		if runErr = runWorkflow(ctx, eng, workflow, rec != nil); runErr != nil {
			break
		}
	}

	if rec == nil {
		return runErr
	}
	if runErr == nil {
		runErr = rec.Finish(ctx, args.results, args.resultsWait, submittedJobs(eng), args.speedup, args.resultsStall, args.bsldFloor)
	}
	if runErr != nil {
		rec.Abort()
	}
	// Cleanup waits for the results so running pods are recorded before they are deleted.
	if err := eng.Reset(context.WithoutCancel(ctx)); runErr == nil {
		runErr = err
	}
	return runErr
}

// runWorkflow runs a workflow; with deferReset it leaves the engine reset to the caller.
func runWorkflow(ctx context.Context, eng *engine.Eng, workflow *config.Workflow, deferReset bool) error {
	if !deferReset {
		return engine.Run(ctx, eng, workflow)
	}
	for _, cfg := range workflow.Tasks {
		if err := eng.RunTask(ctx, cfg); err != nil {
			return err
		}
	}
	return nil
}

// speedUp compresses simulated time: it divides Sleep timeouts and the 'ttl' param of SubmitObj tasks,
// and sets the SubmitTrace speedup, so the results scale by the same factor the run used.
func speedUp(workflows []*config.Workflow, speedup float64) error {
	for _, w := range workflows {
		for _, t := range w.Tasks {
			switch t.Type {
			case engine.TaskSleep:
				scaleDuration(t.Params, "timeout", speedup)
			case engine.TaskSubmitObj:
				if p, ok := t.Params["params"].(map[string]interface{}); ok {
					scaleDuration(p, "ttl", speedup)
				}
			case engine.TaskSubmitTrace:
				if v, ok := t.Params["speedup"]; ok && fmt.Sprint(v) != fmt.Sprint(speedup) {
					return fmt.Errorf("%s: 'speedup' %v differs from -speedup %g; set it with -speedup only", t.ID, v, speedup)
				}
				if t.Params == nil {
					t.Params = map[string]interface{}{}
				}
				t.Params["speedup"] = speedup
			}
		}
	}
	return nil
}

func scaleDuration(m map[string]interface{}, key string, speedup float64) {
	if speedup <= 1 {
		return
	}
	if s, ok := m[key].(string); ok {
		if d, err := time.ParseDuration(s); err == nil {
			m[key] = time.Duration(float64(d) / speedup).String()
		}
	}
}

// splitList splits a flag value on commas and spaces, dropping empty items.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(c rune) bool { return c == ',' || c == ' ' })
}

// workflowsOf lists the workflows in run order for manifest.json, each with the digests of the
// templates its RegisterObj tasks read. A template that cannot be read is left out, since its task
// then fails the run before the results are written.
func workflowsOf(workflows []*config.Workflow) []results.Workflow {
	out := make([]results.Workflow, 0, len(workflows))
	for _, w := range workflows {
		templates := map[string]string{}
		for _, t := range w.Tasks {
			path, ok := t.Params["template"].(string)
			if t.Type != engine.TaskRegisterObj || !ok {
				continue
			}
			if data, err := os.ReadFile(path); err == nil {
				templates[path] = config.Digest(data)
			}
		}
		out = append(out, results.Workflow{Path: w.Path, Digest: w.Digest, Templates: templates})
	}
	return out
}

// submittedJobs lists one job per submitted object that declares a pod name format.
func submittedJobs(eng *engine.Eng) []results.Job {
	var jobs []results.Job
	for _, info := range eng.ObjInfos() {
		if len(info.PodRegexp) != len(info.Names) {
			continue
		}
		for i, name := range info.Names {
			jobs = append(jobs, results.Job{
				Name:      info.Namespace + "/" + name,
				Namespace: info.Namespace,
				Submitted: info.Submitted,
				Pods:      info.PodCount / len(info.Names),
				PodRegexp: info.PodRegexp[i],
				Class:     info.Class,
				Trace:     info.Trace,
			})
		}
	}
	return jobs
}

func aggregate(w io.Writer, pattern, baseline string) error {
	dirs, err := resultDirs(pattern)
	if err != nil {
		return err
	}
	var spreads map[string]results.Spread
	if len(baseline) == 0 {
		spreads, err = results.Aggregate(dirs)
	} else {
		var base []string
		if base, err = resultDirs(baseline); err != nil {
			return err
		}
		spreads, err = results.Compare(dirs, base)
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(spreads)
}

func resultDirs(pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("%q: %v", pattern, err)
	}
	// Skip files such as run logs that sit next to the result directories.
	var dirs []string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.IsDir() {
			dirs = append(dirs, m)
		}
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no directories match %q", pattern)
	}
	return dirs, nil
}

func validate(args *Args) error {
	if len(args.baseline) != 0 && len(args.aggregate) == 0 {
		return fmt.Errorf("'aggregate.baseline' requires 'aggregate'")
	}

	if len(args.workflow) == 0 && args.port == 0 {
		return fmt.Errorf("must specify 'workflow' or 'port'")
	}

	if len(args.workflow) != 0 && args.port > 0 {
		return fmt.Errorf("'workflow' and 'port' are mutually exclusive")
	}

	if len(args.vars) != 0 && len(args.workflow) == 0 {
		return fmt.Errorf("'var' requires 'workflow'")
	}

	// Written as a negation so that NaN, which compares false with everything, is rejected too.
	// +Inf is rejected because results.json and manifest.json cannot encode it.
	if !(args.speedup >= 1) || math.IsInf(args.speedup, 1) {
		return fmt.Errorf("'speedup' must be a finite number, at least 1")
	}

	if args.speedup > 1 && len(args.workflow) == 0 {
		return fmt.Errorf("'speedup' requires 'workflow'")
	}

	if len(args.pushURL) != 0 && len(args.results) == 0 {
		return fmt.Errorf("'results.pushgateway' requires 'results'")
	}

	if len(args.results) != 0 && len(args.workflow) == 0 {
		return fmt.Errorf("'results' requires 'workflow'")
	}

	if len(args.rackLabel) != 0 {
		if len(args.results) == 0 {
			return fmt.Errorf("'results.rack-label' requires 'results'")
		}
		if errs := validation.IsQualifiedName(args.rackLabel); len(errs) != 0 {
			return fmt.Errorf("'results.rack-label' must be a label key such as example.com/rack: %s", errs[0])
		}
	}

	// A lowercase DNS label, as the Suite's profile names are, so that one setup is never filed
	// under two names that differ only in case.
	if len(args.profile) != 0 {
		if len(args.results) == 0 {
			return fmt.Errorf("'results.profile' requires 'results'")
		}
		if errs := validation.IsDNS1123Label(args.profile); len(errs) != 0 {
			return fmt.Errorf("'results.profile' must be a lowercase DNS label: %s", errs[0])
		}
	}

	if len(args.topologyKeys) != 0 {
		if len(args.results) == 0 {
			return fmt.Errorf("'results.topology-labels' requires 'results'")
		}
		for _, key := range splitList(args.topologyKeys) {
			if errs := validation.IsQualifiedName(key); len(errs) != 0 {
				return fmt.Errorf("'results.topology-labels' must list label keys such as example.com/block: %q: %s", key, errs[0])
			}
		}
	}

	return nil
}

func main() {
	defer log.Flush()
	if err := mainInternal(); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}
