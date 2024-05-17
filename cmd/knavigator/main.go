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

	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"

	"github.com/NVIDIA/knavigator/pkg/config"
	"github.com/NVIDIA/knavigator/pkg/engine"
	"github.com/NVIDIA/knavigator/pkg/utils"
)

func mainInternal() error {
	var (
		kubeConfigPath, kubeCtx, taskConfigs string
		qps                                  float64
		burst                                int
		cleanupInfo                          engine.CleanupInfo
	)
	flag.StringVar(&kubeConfigPath, "kubeconfig", "", "kubeconfig file path")
	flag.StringVar(&kubeCtx, "kubectx", "", "kube context")
	flag.BoolVar(&cleanupInfo.Enabled, "cleanup", false, "delete objects")
	flag.DurationVar(&cleanupInfo.Timeout, "cleanup.timeout", engine.DefaultCleanupTimeout, "time limit for cleanup")
	flag.StringVar(&taskConfigs, "tasks", "", "comma-separated list of task config files and dirs")
	flag.Float64Var(&qps, "kube-api-qps", 500, "Maximum QPS to use while talking with Kubernetes API")
	flag.IntVar(&burst, "kube-api-burst", 500, "Maximum burst for throttle while talking with Kubernetes API")

	klog.InitFlags(nil)
	flag.Parse()

	if len(taskConfigs) == 0 {
		flag.Usage()
		return fmt.Errorf("missing task config")
	}

	taskconfigs, err := config.NewFromPaths(taskConfigs)
	if err != nil {
		return err
	}
	if len(taskconfigs) == 0 {
		return fmt.Errorf("missing 'tasks' argument")
	}

	log := textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(utils.Flag2Verbosity(flag.Lookup("v")))))
	cfg := &config.KubeConfig{
		KubeConfigPath: kubeConfigPath,
		KubeCtx:        kubeCtx,
		QPS:            float32(qps),
		Burst:          burst,
	}
	restConfig, err := utils.GetK8sConfig(log, cfg)
	if err != nil {
		return err
	}

	eng, err := engine.New(log, restConfig, &cleanupInfo)
	if err != nil {
		return err
	}

	ctx := context.Background()

	for _, taskconfig := range taskconfigs {
		log.Info("Starting test", "name", taskconfig.Name)
		if err := engine.Run(ctx, eng, taskconfig); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	defer klog.Flush()
	if err := mainInternal(); err != nil {
		klog.Error(err.Error())
		os.Exit(1)
	}
}
