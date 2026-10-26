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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAggregate(t *testing.T) {
	runs := []string{
		`{"pods": 4, "gpu_utilization": 0.5, "bsld_macro": 1.5, "job_wait_sec": {"count": 2, "mean": 3, "p95": 10, "max": 10},
		  "wait_by_footprint": [{"bucket": "17+", "jobs": 2}]}`,
		`{"pods": 4, "gpu_utilization": 0.6, "bsld_macro": null, "job_wait_sec": {"count": 2, "mean": 3, "p95": 12, "max": 12},
		  "wait_by_footprint": [{"bucket": "17+", "jobs": 2}]}`,
	}
	var dirs []string
	for _, r := range runs {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "results.json"), []byte(r), 0o644))
		dirs = append(dirs, dir)
	}

	got, err := Aggregate(dirs)
	require.NoError(t, err)

	pods := got["pods"]
	require.Equal(t, 2, pods.N)
	require.Equal(t, 0.0, *pods.SD)
	require.True(t, pods.Stable, "no variation is stable")

	util := got["gpu_utilization"]
	require.InDelta(t, 0.55, util.Mean, 1e-9)
	require.InDelta(t, 0.0707, *util.SD, 1e-4, "sample standard deviation")
	require.InDelta(t, 0.129, *util.CV, 1e-3)
	require.False(t, util.Stable, "a CV over 5% is unstable")
	require.Equal(t, [2]float64{0.5, 0.6}, util.CI95, "two runs bootstrap to their extremes")

	p95 := got["job_wait_sec{stat=p95}"]
	require.Equal(t, 11.0, p95.Mean)
	require.Equal(t, 10.0, p95.Min)
	require.Equal(t, 12.0, p95.Max)

	require.Equal(t, 2, got["wait_by_footprint_jobs{bucket=17+}"].N)

	bsld := got["bsld_macro"]
	require.Equal(t, 1, bsld.N, "a null counts no run")
	require.Nil(t, bsld.SD)
	require.Nil(t, bsld.CV)
	require.False(t, bsld.Stable, "one run cannot show reproducibility")

	_, err = Aggregate([]string{t.TempDir()})
	require.Error(t, err, "a directory without results.json")
}
