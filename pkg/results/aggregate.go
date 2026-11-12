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
	// VsBaseline compares the metric with the baseline runs; absent without them.
	VsBaseline *Difference `json:"vs_baseline,omitempty"`
}

// Difference compares one metric with the same metric over the baseline runs.
type Difference struct {
	// N is the number of baseline runs that reported the metric.
	N int `json:"n"`
	// Diff is the mean minus the baseline mean.
	Diff float64 `json:"diff"`
	// CI95 is a percentile bootstrap interval for Diff; null when only one run on either side
	// reports the metric.
	CI95 *[2]float64 `json:"ci95"`
	// Clear is set when CI95 excludes 0 and at least 5 runs on each side report the metric.
	Clear bool `json:"clear"`
}

// minRunsToCompare is the fewest runs on each side, reporting the metric, for a clear difference.
const minRunsToCompare = 5

// Aggregate reads results.json from each directory and summarizes every metric across them. A
// metric that a run left out or reported as null counts only the runs that have it.
func Aggregate(dirs []string) (map[string]Spread, error) {
	r, err := readRuns(dirs)
	if err != nil {
		return nil, err
	}
	warnMixedTraces(r.traces, r.untraced, len(dirs), "runs")
	out := make(map[string]Spread, len(r.values))
	for k, v := range r.values {
		out[k] = spread(v)
	}
	return out, nil
}

// Compare aggregates dirs like Aggregate and adds each metric's difference from the baseline runs.
// The difference is the configuration's only when the runs replayed the same trace, and the two
// sides are independent only when no run is on both, so it refuses runs with different
// trace_digest values, on one side or across the two, and a run on both sides; runs without one
// are compared, with a warning when other runs have one.
func Compare(dirs, baseline []string) (map[string]Spread, error) {
	r, err := readRuns(dirs)
	if err != nil {
		return nil, err
	}
	b, err := readRuns(baseline)
	if err != nil {
		return nil, err
	}
	for i, bf := range b.files {
		for _, rf := range r.files {
			if os.SameFile(rf, bf) {
				return nil, fmt.Errorf("%s is both a run and a baseline run", baseline[i])
			}
		}
	}
	for _, side := range []struct {
		name   string
		traces map[string]bool
	}{{"runs", r.traces}, {"baseline runs", b.traces}} {
		if len(side.traces) > 1 {
			return nil, fmt.Errorf("the %s did not all replay the same traces (%s), so they cannot be compared",
				side.name, strings.Join(slices.Sorted(maps.Keys(side.traces)), "; "))
		}
	}
	switch {
	case len(r.traces) == 1 && len(b.traces) == 1 && !maps.Equal(r.traces, b.traces):
		return nil, fmt.Errorf("the runs replayed trace %s and the baseline %s, so they cannot be compared",
			slices.Collect(maps.Keys(r.traces))[0], slices.Collect(maps.Keys(b.traces))[0])
	case (len(r.traces) == 0) != (len(b.traces) == 0):
		log.Warningf("Aggregate: only one side reports a trace_digest, so the traces were not compared")
	}
	warnMixedTraces(r.traces, r.untraced, len(dirs), "runs")
	warnMixedTraces(b.traces, b.untraced, len(baseline), "baseline runs")
	out := make(map[string]Spread, len(r.values))
	for k, v := range r.values {
		s := spread(v)
		if bv, ok := b.values[k]; ok {
			s.VsBaseline = difference(v, bv)
		}
		out[k] = s
	}
	return out, nil
}

// runSet is what readRuns reads from a set of runs.
type runSet struct {
	values map[string][]float64
	// traces holds each run's whole trace_digest; untraced counts the runs without one.
	traces   map[string]bool
	untraced int
	// files holds each run's results.json, in the order of the directories.
	files []os.FileInfo
}

func readRuns(dirs []string) (runSet, error) {
	r := runSet{values: map[string][]float64{}, traces: map[string]bool{}}
	for _, dir := range dirs {
		path := filepath.Join(dir, "results.json")
		info, err := os.Stat(path)
		if err != nil {
			return runSet{}, err
		}
		r.files = append(r.files, info)
		data, err := os.ReadFile(path)
		if err != nil {
			return runSet{}, err
		}
		var tree interface{}
		if err := json.Unmarshal(data, &tree); err != nil {
			return runSet{}, fmt.Errorf("%s: %w", dir, err)
		}
		for _, s := range samples(tree) {
			k := metricKey(s)
			r.values[k] = append(r.values[k], s.value)
		}
		digest := ""
		if m, ok := tree.(map[string]interface{}); ok {
			digest, _ = m["trace_digest"].(string)
		}
		if digest == "" {
			r.untraced++
		} else {
			r.traces[digest] = true
		}
	}
	return r, nil
}

// warnMixedTraces logs when the runs did not all replay the same traces. traces holds each run's
// whole trace_digest, since runs with the same several traces match. untraced counts the others.
func warnMixedTraces(traces map[string]bool, untraced, runs int, side string) {
	if len(traces) > 1 {
		each := map[string]bool{}
		for digest := range traces {
			for _, d := range strings.Split(digest, ",") {
				each[d] = true
			}
		}
		log.Warningf("Aggregate: the %s replayed %d different traces (%s), so the spread includes the difference between them",
			side, len(each), strings.Join(slices.Sorted(maps.Keys(each)), ", "))
	}
	if untraced > 0 && len(traces) > 0 {
		log.Warningf("Aggregate: no trace_digest in %d of %d %s, so the spread may include the difference between traces", untraced, runs, side)
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

// difference bootstraps mean(a) - mean(b), resampling each side on its own, since the runs of two
// configurations are not paired.
func difference(a, b []float64) *Difference {
	d := &Difference{N: len(b)}
	// When every run on each side reports one value, as gpu_capacity does, the difference is exact;
	// summing the copies would round differently for different run counts.
	exact := constant(a) && constant(b)
	if exact {
		d.Diff = a[0] - b[0]
	} else {
		d.Diff = mean(a) - mean(b)
	}
	if len(a) < 2 || len(b) < 2 {
		return d
	}
	ci := [2]float64{d.Diff, d.Diff}
	if !exact {
		rng := rand.New(rand.NewPCG(bootstrapSeed, 0))
		diffs := make([]float64, bootstrapResamples)
		for i := range diffs {
			diffs[i] = resampledMean(rng, a) - resampledMean(rng, b)
		}
		ci = percentile95(diffs)
	}
	d.CI95 = &ci
	d.Clear = (ci[0] > 0 || ci[1] < 0) && min(len(a), len(b)) >= minRunsToCompare
	return d
}

func constant(v []float64) bool {
	for _, x := range v {
		if x != v[0] {
			return false
		}
	}
	return true
}

// bootstrapMean resamples v with replacement and returns the middle 95% of the resampled means,
// cutting the same count from each tail.
func bootstrapMean(v []float64) [2]float64 {
	rng := rand.New(rand.NewPCG(bootstrapSeed, 0))
	means := make([]float64, bootstrapResamples)
	for i := range means {
		means[i] = resampledMean(rng, v)
	}
	return percentile95(means)
}

func resampledMean(rng *rand.Rand, v []float64) float64 {
	var sum float64
	for range v {
		sum += v[rng.IntN(len(v))]
	}
	return sum / float64(len(v))
}

// percentile95 sorts x and returns its middle 95%, cutting the same count from each tail.
func percentile95(x []float64) [2]float64 {
	sort.Float64s(x)
	k := int(math.Floor(float64(len(x))*0.05/2 + 1e-9))
	return [2]float64{x[k], x[len(x)-1-k]}
}
