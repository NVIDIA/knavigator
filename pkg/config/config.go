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
	"regexp"
	"slices"
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

// Vars holds -var values for ${name} and ${name:-default} references in workflow files.
// $${name} stays a literal ${name}, and a default cannot contain '}'.
type Vars map[string]string

var (
	varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	varRef  = regexp.MustCompile(`\$?\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)
)

// Set implements flag.Value, so -var name=value can repeat.
func (v Vars) Set(s string) error {
	name, value, ok := strings.Cut(s, "=")
	if !ok || !varName.MatchString(name) {
		return fmt.Errorf("want name=value, got %q", s)
	}
	v[name] = value
	return nil
}

func (v Vars) String() string {
	pairs := make([]string, 0, len(v))
	for name, value := range v {
		pairs = append(pairs, name+"="+value)
	}
	slices.Sort(pairs)
	return strings.Join(pairs, ",")
}

// expand replaces ${name} and ${name:-default} in data before YAML parsing, so values keep
// their YAML types. It records each var it uses in used.
func (v Vars) expand(data []byte, used map[string]bool) ([]byte, error) {
	var missing []string
	out := varRef.ReplaceAllFunc(data, func(ref []byte) []byte {
		if ref[1] == '$' {
			return ref[1:]
		}
		m := varRef.FindSubmatch(ref)
		name := string(m[1])
		if value, ok := v[name]; ok {
			used[name] = true
			return []byte(value)
		}
		if m[2] != nil {
			return m[2]
		}
		missing = append(missing, name)
		return ref
	})
	if len(missing) != 0 {
		slices.Sort(missing)
		missing = slices.Compact(missing)
		return nil, fmt.Errorf("no value for %s; pass -var name=value", strings.Join(missing, ", "))
	}
	return out, nil
}

// unused returns an error naming vars that no workflow referenced, which catches typos.
func (v Vars) unused(used map[string]bool) error {
	var names []string
	for name := range v {
		if !used[name] {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	slices.Sort(names)
	return fmt.Errorf("no workflow uses -var %s", strings.Join(names, ", "))
}

// NewFromFile populates workflow config from YAML file, after expanding vars
func NewFromFile(path string, vars Vars) (*Workflow, error) {
	used := map[string]bool{}
	cfg, err := newFromFile(path, vars, used)
	if err != nil {
		return nil, err
	}
	return cfg, vars.unused(used)
}

func newFromFile(path string, vars Vars, used map[string]bool) (*Workflow, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if data, err = vars.expand(data, used); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return New(data)
}

// NewFromPaths populates workflow configs from files and dirs, after expanding vars
func NewFromPaths(paths string, vars Vars) ([]*Workflow, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("empty filepaths")
	}

	files, err := parsePaths(paths)
	if err != nil {
		return nil, err
	}

	configs := []*Workflow{}
	used := map[string]bool{}
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
					if cfg, err = newFromFile(filepath.Join(path, file.Name()), vars, used); err != nil {
						return nil, err
					}
					configs = append(configs, cfg)
				}
			}
		} else {
			if cfg, err = newFromFile(path, vars, used); err != nil {
				return nil, err
			}
			configs = append(configs, cfg)
		}
	}

	return configs, vars.unused(used)
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
