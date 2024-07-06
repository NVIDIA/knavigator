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

	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/engine"
	"github.com/NVIDIA/knavigator/pkg/server"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

type Args struct {
	kubeCfg     config.KubeConfig
	workflow    string
	port        int
	cleanupInfo engine.CleanupInfo
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

	for _, workflow := range workflows {
		log.Infof("Starting workflow %s", workflow.Name)
		if err := engine.Run(ctx, eng, workflow); err != nil {
			return err
		}
	}

	return nil
}

func validate(args *Args) error {
	if len(args.workflow) == 0 && args.port == 0 {
		return fmt.Errorf("must specify 'workflow' or 'port'")
	}

	if len(args.workflow) != 0 && args.port > 0 {
		return fmt.Errorf("'workflow' and 'port' are mutually exclusive")
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
