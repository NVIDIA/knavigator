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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/engine"
	"github.com/NVIDIA/knavigator/pkg/results"
	"github.com/NVIDIA/knavigator/pkg/server"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

type Args struct {
	kubeCfg     config.KubeConfig
	workflow    string
	port        int
	cleanupInfo engine.CleanupInfo
	results     string
	resultsWait time.Duration
}

func mainInternal() error {
	var args Args
	flag.StringVar(&args.kubeCfg.KubeConfigPath, "kubeconfig", "", "kubeconfig file path")
	flag.StringVar(&args.kubeCfg.KubeCtx, "kubectx", "", "kube context")
	flag.Float64Var(&args.kubeCfg.QPS, "kube-api-qps", 500, "Maximum QPS to use while talking with Kubernetes API")
	flag.IntVar(&args.kubeCfg.Burst, "kube-api-burst", 500, "Maximum burst for throttle while talking with Kubernetes API")
	flag.BoolVar(&args.cleanupInfo.Enabled, "cleanup", false, "delete objects")
	flag.DurationVar(&args.cleanupInfo.Timeout, "cleanup.timeout", engine.DefaultCleanupTimeout, "time limit for cleanup")
	flag.StringVar(&args.workflow, "workflow", "", "comma-separated list of workflow config files and dirs (mutually exclusive with the 'port' flag)")
	flag.IntVar(&args.port, "port", 0, "listening port (mutually exclusive with the 'workflow' flag)")
	flag.StringVar(&args.results, "results", "", "directory for run results; waits for submitted pods to finish (requires 'workflow')")
	flag.DurationVar(&args.resultsWait, "results.timeout", time.Hour, "time limit for pods to finish before writing results")

	log.InitFlags(nil)
	flag.Parse()

	if err := validate(&args); err != nil {
		flag.Usage()
		return err
	}

	restConfig, err := utils.GetK8sConfig(&args.kubeCfg)
	if err != nil {
		return err
	}

	eng, err := engine.New(restConfig, &args.cleanupInfo)
	if err != nil {
		return err
	}

	if args.port > 0 {
		return server.New(eng, args.port).Run()
	}

	workflows, err := config.NewFromPaths(args.workflow)
	if err != nil {
		return err
	}

	ctx := context.Background()

	var rec *results.Recorder
	if len(args.results) != 0 {
		client, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return err
		}
		if rec, err = results.Start(ctx, client); err != nil {
			return err
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
		runErr = rec.Finish(ctx, args.results, args.resultsWait, submittedJobs(eng))
	}
	// Cleanup waits for the results so running pods are recorded before they are deleted.
	if err := eng.Reset(ctx); runErr == nil {
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
			})
		}
	}
	return jobs
}

func validate(args *Args) error {
	if len(args.workflow) == 0 && args.port == 0 {
		return fmt.Errorf("must specify 'workflow' or 'port'")
	}

	if len(args.workflow) != 0 && args.port > 0 {
		return fmt.Errorf("'workflow' and 'port' are mutually exclusive")
	}

	if len(args.results) != 0 && len(args.workflow) == 0 {
		return fmt.Errorf("'results' requires 'workflow'")
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
