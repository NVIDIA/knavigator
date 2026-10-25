/*
 * Copyright (c) 2026, NVIDIA CORPORATION & AFFILIATES. All rights reserved.
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

// Package claims loads a scheduler's documented claims: for each
// capability, what the scheduler's documentation says, where, the verdict
// that claim implies, and the scheduler mechanism a probe uses to exercise
// it. A run's verdicts are judged against these claims.
package claims

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/NVIDIA/knavigator/pkg/verdict"
)

// Claim is one capability's documented claim.
type Claim struct {
	Claim     string          `json:"claim"`
	Citation  string          `json:"citation"`
	Expected  verdict.Outcome `json:"expected"`
	Mechanism string          `json:"mechanism"`
}

// File is one scheduler's claims file, such as resources/claims/kueue.yaml.
type File struct {
	Scheduler string           `json:"scheduler"`
	Version   string           `json:"version"`
	Claims    map[string]Claim `json:"claims"`
}

// Parse decodes and validates a claims file: every capability in
// verdict.Capabilities must have a non-empty claim and mechanism, an https
// citation and a valid expected outcome, and no other capability may appear.
func Parse(data []byte) (File, error) {
	var f File
	if err := yaml.UnmarshalStrict(data, &f); err != nil {
		return File{}, fmt.Errorf("claims: %w", err)
	}
	if f.Scheduler == "" || f.Version == "" {
		return File{}, errors.New("claims: scheduler and version must both be set")
	}
	known := map[string]bool{}
	for _, c := range verdict.Capabilities {
		known[c] = true
	}
	names := make([]string, 0, len(f.Claims))
	for name := range f.Claims {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !known[name] {
			return File{}, fmt.Errorf("claims: unknown capability %q", name)
		}
	}
	for _, c := range verdict.Capabilities {
		cl, ok := f.Claims[c]
		switch {
		case !ok:
			return File{}, fmt.Errorf("claims: missing capability %q", c)
		case strings.TrimSpace(cl.Claim) == "":
			return File{}, fmt.Errorf("claims: capability %q: claim is empty", c)
		case !strings.HasPrefix(cl.Citation, "https://"):
			return File{}, fmt.Errorf("claims: capability %q: citation %q is not an https URL", c, cl.Citation)
		case !cl.Expected.Valid():
			return File{}, fmt.Errorf("claims: capability %q: expected %q, want supported, partial or not_supported", c, cl.Expected)
		case strings.TrimSpace(cl.Mechanism) == "":
			return File{}, fmt.Errorf("claims: capability %q: mechanism is empty", c)
		}
	}
	return f, nil
}

// Load reads and validates the claims file at path.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	f, err := Parse(data)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}
