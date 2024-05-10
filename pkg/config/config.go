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

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type TaskConfig struct {
	Name        string  `yaml:"name"`
	Description string  `yaml:"description,omitempty"`
	Tasks       []*Task `yaml:"tasks"`
}

type Task struct {
	ID          string                 `yaml:"id"`
	Type        string                 `yaml:"type"`
	Description string                 `yaml:"description,omitempty"`
	Params      map[string]interface{} `yaml:"params,omitempty"`
}

// New populates task config from raw data
func New(data []byte) (*TaskConfig, error) {
	var config TaskConfig

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// NewFromFile populates test config from YAML file
func NewFromFile(path string) (*TaskConfig, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return New(data)
}

func NewFromPaths(paths string) ([]*TaskConfig, error) {
	cfgPaths := strings.Split(paths, ",")
	configs := []*TaskConfig{}
	var cfg *TaskConfig
	for _, path := range cfgPaths {
		fileInfo, err := os.Stat(path)
		if err != nil {
			return nil, err
		}

		if fileInfo.IsDir() {
			files, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if !file.IsDir() {
					if cfg, err = NewFromFile(filepath.Join(path, file.Name())); err != nil {
						return nil, err
					}
					configs = append(configs, cfg)
				}
			}
		} else {
			if cfg, err = NewFromFile(path); err != nil {
				return nil, err
			}
			configs = append(configs, cfg)
		}
	}

	return configs, nil
}

// validate performs checks of mandatory fields
func (c *TaskConfig) validate() error {
	if len(c.Tasks) == 0 {
		return fmt.Errorf("test %s has no tasks", c.Name)
	}
	for i, task := range c.Tasks {
		if len(task.ID) == 0 {
			return fmt.Errorf("missing task ID for tasks[%d]", i)
		}
		if len(task.Type) == 0 {
			return fmt.Errorf("missing task type for tasks[%d]", i)
		}
	}
	return nil
}
