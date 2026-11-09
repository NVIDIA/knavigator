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

package results

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	log "k8s.io/klog/v2"
)

const (
	// cvTolerance is the largest coefficient of variation a stable metric may have.
	cvTolerance = 0.05
	// Seeded, so the same runs always give the same interval.
	bootstrapResamples = 4000
	bootstrapSeed      = 0
)

// Spread summarizes one metric across repeated runs.
type Spread struct {
	N    int     `json:"n"`
	Mean float64 `json:"mean"`
	// SD is the sample standard deviation; null for one run, which cannot show reproducibility.
	SD  *float64 `json:"sd"`
	Min float64  `json:"min"`
	Max float64  `json:"max"`
	// CV is SD / |Mean|; null when SD is, or when the mean is 0.
	CV *float64 `json:"cv"`
	// CI95 is a percentile bootstrap interval for the mean.
	CI95 [2]float64 `json:"ci95"`
	// Stable is set when at least two runs agree: no variation, or a CV of at most 5%.
	Stable bool `json:"stable"`
}

// Aggregate reads results.json from each directory and summarizes every metric across them. A
// metric that a run left out or reported as null counts only the runs that have it.
func Aggregate(dirs []string) (map[string]Spread, error) {
	values := map[string][]float64{}
	traces := map[string]bool{}
	untraced := 0
	for _, dir := range dirs {
		data, err := os.ReadFile(filepath.Join(dir, "results.json"))
		if err != nil {
			return nil, err
		}
		var tree interface{}
		if err := json.Unmarshal(data, &tree); err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		for _, s := range samples(tree) {
			k := metricKey(s)
			values[k] = append(values[k], s.value)
		}
		digest := ""
		if m, ok := tree.(map[string]interface{}); ok {
			digest, _ = m["trace_digest"].(string)
		}
		if digest == "" {
			untraced++
		} else {
			traces[digest] = true
		}
	}
	warnMixedTraces(traces, untraced, len(dirs))

	out := make(map[string]Spread, len(values))
	for k, v := range values {
		out[k] = spread(v)
	}
	return out, nil
}

// warnMixedTraces logs when the runs did not all replay the same traces. traces holds each run's
// whole trace_digest, since runs with the same several traces match. untraced counts the others.
func warnMixedTraces(traces map[string]bool, untraced, runs int) {
	if len(traces) > 1 {
		each := map[string]bool{}
		for digest := range traces {
			for _, d := range strings.Split(digest, ",") {
				each[d] = true
			}
		}
		log.Warningf("Aggregate: the runs replayed %d different traces (%s), so the spread includes the difference between them",
			len(each), strings.Join(slices.Sorted(maps.Keys(each)), ", "))
	}
	if untraced > 0 && len(traces) > 0 {
		log.Warningf("Aggregate: no trace_digest in %d of %d runs, so the spread may include the difference between traces", untraced, runs)
	}
}

// metricKey names a sample like an unquoted Prometheus series, such as job_wait_sec{stat=p95}.
func metricKey(s sample) string {
	name := strings.TrimPrefix(s.name, metricsRoot)
	if len(s.labels) == 0 {
		return name
	}
	labels := make([]string, 0, len(s.labels))
	for k, v := range s.labels {
		labels = append(labels, k+"="+v)
	}
	sort.Strings(labels)
	return name + "{" + strings.Join(labels, ",") + "}"
}

func spread(v []float64) Spread {
	s := Spread{N: len(v), Min: v[0], Max: v[0]}
	var sum float64
	for _, x := range v {
		sum += x
		s.Min, s.Max = math.Min(s.Min, x), math.Max(s.Max, x)
	}
	s.Mean = sum / float64(len(v))
	s.CI95 = bootstrapMean(v)
	if len(v) < 2 {
		return s
	}
	var ss float64
	for _, x := range v {
		ss += (x - s.Mean) * (x - s.Mean)
	}
	sd := math.Sqrt(ss / float64(len(v)-1))
	s.SD = &sd
	if s.Mean != 0 {
		cv := sd / math.Abs(s.Mean)
		s.CV = &cv
	}
	s.Stable = sd == 0 || (s.CV != nil && *s.CV <= cvTolerance)
	return s
}

// bootstrapMean resamples v with replacement and returns the middle 95% of the resampled means,
// cutting the same count from each tail.
func bootstrapMean(v []float64) [2]float64 {
	rng := rand.New(rand.NewPCG(bootstrapSeed, 0))
	means := make([]float64, bootstrapResamples)
	for i := range means {
		var sum float64
		for range v {
			sum += v[rng.IntN(len(v))]
		}
		means[i] = sum / float64(len(v))
	}
	sort.Float64s(means)
	k := int(math.Floor(bootstrapResamples*0.05/2 + 1e-9))
	return [2]float64{means[k], means[bootstrapResamples-1-k]}
}
