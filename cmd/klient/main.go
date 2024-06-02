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
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
)

func mainInternal() error {
	var addr, workflow string
	flag.StringVar(&addr, "address", "", "server address")
	flag.StringVar(&workflow, "workflow", "", "comma-separated list of workflow config files and dirs")
	flag.Parse()

	if len(addr) == 0 {
		return fmt.Errorf("missing 'address' argument")
	}
	if len(workflow) == 0 {
		return fmt.Errorf("missing 'workload' argument")
	}

	workflows, err := config.NewFromPaths(workflow)
	if err != nil {
		return err
	}

	urlPath, err := url.JoinPath(addr, "workflow")
	if err != nil {
		return err
	}

	for _, workflow := range workflows {
		fmt.Printf("Starting workflow %s\n", workflow.Name)
		if err := execWorkflow(urlPath, workflow); err != nil {
			return err
		}
	}

	return nil
}

func execWorkflow(urlPath string, workflow *config.Workflow) error {

	data, err := yaml.Marshal(workflow)
	if err != nil {
		return fmt.Errorf("failed to marshal to YAML: %v", err)
	}

	resp, err := http.Post(urlPath, "application/x-yaml", bytes.NewBuffer(data)) // #nosec G107 // Potential HTTP request made with variable url
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	defer resp.Body.Close() //nolint:errcheck // No check for the return value of Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error response: %s: %s\n", resp.Status, body)
		return fmt.Errorf("%s", resp.Status)
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
