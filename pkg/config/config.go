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

type KubeConfig struct {
	KubeConfigPath string
	KubeCtx        string
	QPS            float64
	Burst          int
}

type Workflow struct {
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

// New populates workflow config from raw data
func New(data []byte) (*Workflow, error) {
	var config Workflow

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// NewFromFile populates workflow config from YAML file
func NewFromFile(path string) (*Workflow, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return New(data)
}

func NewFromPaths(paths string) ([]*Workflow, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("empty filepaths")
	}

	files, err := parsePaths(paths)
	if err != nil {
		return nil, err
	}

	configs := []*Workflow{}
	var cfg *Workflow
	for _, path := range files {
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
func (c *Workflow) validate() error {
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

func parsePaths(paths string) ([]string, error) {
	start, n := 0, len(paths)
	braces := false
	ret := []string{}

	for i := 1; i < n; i++ {
		switch paths[i] {
		case '{':
			if braces {
				return nil, fmt.Errorf("unbalanced braces in %q", paths)
			}
			braces = true
		case '}':
			if !braces {
				return nil, fmt.Errorf("unbalanced braces in %q", paths)
			}
			braces = false
		case ',':
			if !braces {
				ret = append(ret, expandBraces(strings.TrimSpace(paths[start:i]))...)
				start = i + 1
			}
		}
	}
	if braces {
		return nil, fmt.Errorf("unbalanced braces in %q", paths)
	}
	if start < n {
		ret = append(ret, expandBraces(strings.TrimSpace(paths[start:n]))...)
	}

	return ret, nil
}

func expandBraces(pattern string) []string {
	start := strings.Index(pattern, "{")
	if start == -1 {
		return []string{pattern}
	}
	end := strings.Index(pattern[start:], "}")
	if end == -1 {
		return []string{pattern}
	}
	end += start
	prefix := pattern[:start]
	suffix := pattern[end+1:]
	parts := strings.Split(pattern[start+1:end], ",")

	var res []string
	for _, part := range parts {
		res = append(res, expandBraces(prefix+part+suffix)...)
	}
	return res
}
