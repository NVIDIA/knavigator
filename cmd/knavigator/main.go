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

func usage() {
	msg := `Usage: bin/knavigagor <--tasks... > [options...]
            --kubeconfig  kubeconfig file path
            --kubectx     kube context 
            --tasks       comma-separated list of task config files and dirs`
	fmt.Println(msg)
}

func mainInternal() error {
	var kubeConfigPath, kubeCtx, taskConfigs string
	flag.StringVar(&kubeConfigPath, "kubeconfig", "", "kubeconfig file path")
	flag.StringVar(&kubeCtx, "kubectx", "", "kube context")
	flag.StringVar(&taskConfigs, "tasks", "", "comma-separated list of task config files and dirs")

	klog.InitFlags(nil)
	flag.Parse()

	taskconfigs, err := config.NewFromPaths(taskConfigs)
	if err != nil {
		usage()
		return err
	}
	if len(taskconfigs) == 0 {
		return fmt.Errorf("missing 'tasks' argument")
	}

	log := textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(utils.Flag2Verbosity(flag.Lookup("v")))))

	restConfig, err := utils.GetK8sConfig(log, kubeConfigPath, kubeCtx)
	if err != nil {
		return err
	}

	eng, err := engine.New(log, restConfig)
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
