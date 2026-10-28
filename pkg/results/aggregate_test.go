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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	log "k8s.io/klog/v2"
)

func TestAggregate(t *testing.T) {
	runs := []string{
		`{"trace_digest": "3f2a9c41d07e", "pods": 4, "gpu_utilization": 0.5, "bsld_macro": 1.5, "job_wait_sec": {"count": 2, "mean": 3, "p95": 10, "max": 10},
		  "wait_by_footprint": [{"bucket": "17+", "jobs": 2}]}`,
		`{"trace_digest": "3f2a9c41d07e", "pods": 4, "gpu_utilization": 0.6, "bsld_macro": null, "job_wait_sec": {"count": 2, "mean": 3, "p95": 12, "max": 12},
		  "wait_by_footprint": [{"bucket": "17+", "jobs": 2}]}`,
	}
	dirs := writeRuns(t, runs...)

	got, err := Aggregate(dirs)
	require.NoError(t, err)
	require.NotContains(t, got, "trace_digest")

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

	buf := captureLog(t)
	_, err = Aggregate(dirs)
	require.NoError(t, err)
	log.Flush()
	require.NotContains(t, buf.String(), "different traces", "both runs replayed one trace")

	require.NoError(t, os.WriteFile(filepath.Join(dirs[1], "results.json"), []byte(`{"trace_digest": "08c1e5b7aa20", "pods": 4}`), 0o644))
	_, err = Aggregate(dirs)
	require.NoError(t, err)
	log.Flush()
	require.Contains(t, buf.String(), "the runs replayed 2 different traces (08c1e5b7aa20, 3f2a9c41d07e)")

	require.NoError(t, os.WriteFile(filepath.Join(dirs[1], "results.json"), []byte(`{"pods": 4}`), 0o644))
	_, err = Aggregate(dirs)
	require.NoError(t, err)
	log.Flush()
	require.Contains(t, buf.String(), "no trace_digest in 1 of 2 runs")

	_, err = Aggregate([]string{t.TempDir()})
	require.Error(t, err, "a directory without results.json")
}

func TestWarnMixedTraces(t *testing.T) {
	tests := []struct {
		name     string
		traces   []string
		untraced int
		want     []string
		wantNot  []string
	}{
		{name: "one trace", traces: []string{"a"}, wantNot: []string{"different traces", "no trace_digest"}},
		{name: "same two traces in every run", traces: []string{"a,b"}, wantNot: []string{"different traces"}},
		{name: "two traces", traces: []string{"a", "b"}, want: []string{"replayed 2 different traces (a, b)"}},
		{name: "a run with several traces", traces: []string{"a,b", "a"}, want: []string{"replayed 2 different traces (a, b)"}},
		{name: "runs with several traces each", traces: []string{"a,b", "b,c"}, want: []string{"replayed 3 different traces (a, b, c)"}},
		{name: "some runs without a digest", traces: []string{"a"}, untraced: 1, want: []string{"no trace_digest in 1 of 2 runs"}, wantNot: []string{"different traces"}},
		{name: "no run has a digest", untraced: 2, wantNot: []string{"different traces", "no trace_digest"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := captureLog(t)
			traces := map[string]bool{}
			for _, d := range tt.traces {
				traces[d] = true
			}
			warnMixedTraces(traces, tt.untraced, len(tt.traces)+tt.untraced, "runs")
			log.Flush()
			for _, w := range tt.want {
				require.Contains(t, buf.String(), w)
			}
			for _, w := range tt.wantNot {
				require.NotContains(t, buf.String(), w)
			}
		})
	}
}

func writeRuns(t *testing.T, runs ...string) []string {
	t.Helper()
	var dirs []string
	for _, r := range runs {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "results.json"), []byte(r), 0o644))
		dirs = append(dirs, dir)
	}
	return dirs
}

func TestCompare(t *testing.T) {
	run := func(trace string, makespan, util float64) string {
		return fmt.Sprintf(`{"trace_digest": %q, "makespan_sec": %g, "gpu_utilization": %g, "pods": 4}`, trace, makespan, util)
	}
	var a, b []string
	for i := range 5 {
		a = append(a, run("t1", 100+float64(i), 0.5+0.1*float64(i)))
		b = append(b, run("t1", 120+float64(i), 0.6+0.1*float64(i)))
	}
	got, err := Compare(writeRuns(t, a...), writeRuns(t, b...))
	require.NoError(t, err)

	makespan := got["makespan_sec"].VsBaseline
	require.Equal(t, 5, makespan.N)
	require.Equal(t, -20.0, makespan.Diff)
	require.Less(t, makespan.CI95[1], 0.0)
	require.True(t, makespan.Clear)

	util := got["gpu_utilization"].VsBaseline
	require.InDelta(t, -0.1, util.Diff, 1e-12)
	require.False(t, util.Clear, "a gap smaller than the spread is not clear")
	require.False(t, got["pods"].VsBaseline.Clear, "identical runs differ by nothing")

	makespanRuns := func(makespans ...float64) []string {
		var runs []string
		for _, m := range makespans {
			runs = append(runs, run("t1", m, 0.5))
		}
		return writeRuns(t, runs...)
	}
	got, err = Compare(makespanRuns(101, 97, 112, 104, 93), makespanRuns(120, 131, 118, 126, 122))
	require.NoError(t, err)
	require.InDelta(t, -22.0, got["makespan_sec"].VsBaseline.Diff, 1e-9)
	ci := got["makespan_sec"].VsBaseline.CI95
	require.InDelta(t, -28.8, ci[0], 1e-9, "the same runs always give the same interval")
	require.InDelta(t, -15.2, ci[1], 1e-9)

	got, err = Compare(writeRuns(t, b...), writeRuns(t, a...))
	require.NoError(t, err)
	require.Equal(t, 20.0, got["makespan_sec"].VsBaseline.Diff)
	require.True(t, got["makespan_sec"].VsBaseline.Clear, "a gap either way is clear")

	got, err = Compare(writeRuns(t, a[:4]...), writeRuns(t, b...))
	require.NoError(t, err)
	require.Less(t, got["makespan_sec"].VsBaseline.CI95[1], 0.0)
	require.False(t, got["makespan_sec"].VsBaseline.Clear, "four runs on one side are too few")

	got, err = Compare(writeRuns(t, a...), writeRuns(t, b[:1]...))
	require.NoError(t, err)
	require.Nil(t, got["makespan_sec"].VsBaseline.CI95, "one baseline run has no interval")
	got, err = Compare(writeRuns(t, a[:1]...), writeRuns(t, b...))
	require.NoError(t, err)
	require.Nil(t, got["makespan_sec"].VsBaseline.CI95, "one run has no interval")

	got, err = Compare(writeRuns(t, a[:2]...), writeRuns(t, b[:2]...))
	require.NoError(t, err)
	require.NotNil(t, got["makespan_sec"].VsBaseline.CI95)
	require.False(t, got["makespan_sec"].VsBaseline.Clear, "two runs a side are too few")

	buf := captureLog(t)
	got, err = Compare(writeRuns(t, a[:1]...), writeRuns(t, `{"makespan_sec": 130}`))
	require.NoError(t, err)
	log.Flush()
	require.Contains(t, buf.String(), "only one side reports a trace_digest")
	require.Equal(t, -30.0, got["makespan_sec"].VsBaseline.Diff)
	require.Nil(t, got["makespan_sec"].VsBaseline.CI95)
	require.Nil(t, got["pods"].VsBaseline, "the baseline has no pods")

	// The same 1/3 in every run averages to slightly different floats over 5 and 10 runs.
	third := `{"gpu_utilization": 0.3333333333333333}`
	got, err = Compare(writeRuns(t, slices.Repeat([]string{third}, 5)...), writeRuns(t, slices.Repeat([]string{third}, 10)...))
	require.NoError(t, err)
	require.Zero(t, got["gpu_utilization"].VsBaseline.Diff)
	require.False(t, got["gpu_utilization"].VsBaseline.Clear)

	got, err = Compare(writeRuns(t, a...), writeRuns(t, append(b, `{"makespan_sec": 200}`)...))
	require.NoError(t, err, "a baseline run without a digest warns but does not refuse")
	log.Flush()
	require.Contains(t, buf.String(), "no trace_digest in 1 of 6 baseline runs")
	require.Equal(t, 6, got["makespan_sec"].VsBaseline.N)

	_, err = Compare(writeRuns(t, append(a, `{"makespan_sec": 200}`)...), writeRuns(t, b...))
	require.NoError(t, err)
	log.Flush()
	require.Contains(t, buf.String(), "no trace_digest in 1 of 6 runs")

	buf = captureLog(t)
	_, err = Compare(writeRuns(t, `{"makespan_sec": 130}`), writeRuns(t, b...))
	require.NoError(t, err)
	log.Flush()
	require.Contains(t, buf.String(), "only one side reports a trace_digest")

	// The same two traces in different shares on each side would read their difference as clear.
	_, err = Compare(writeRuns(t, slices.Concat(a[:4], []string{run("t2", 160, 0.6)})...), writeRuns(t, b...))
	require.EqualError(t, err, "the runs did not all replay the same traces (t1; t2), so they cannot be compared")
	both := run("t1,t2", 100, 0.5)
	_, err = Compare(writeRuns(t, both), writeRuns(t, both, run("t1", 120, 0.6)))
	require.EqualError(t, err, "the baseline runs did not all replay the same traces (t1; t1,t2), so they cannot be compared")
	_, err = Compare(writeRuns(t, both, both), writeRuns(t, both, both))
	require.NoError(t, err, "every run replayed the same two traces")

	dirs := writeRuns(t, a...)
	_, err = Compare(dirs, dirs[3:])
	require.EqualError(t, err, dirs[3]+" is both a run and a baseline run")
	t.Chdir(filepath.Dir(dirs[3]))
	_, err = Compare(dirs, []string{filepath.Base(dirs[3])})
	require.EqualError(t, err, filepath.Base(dirs[3])+" is both a run and a baseline run", "relative and absolute")
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(dirs[3], link))
	_, err = Compare(dirs, []string{link})
	require.EqualError(t, err, link+" is both a run and a baseline run")

	_, err = Compare(writeRuns(t, a...), writeRuns(t, run("t2", 120, 0.6)))
	require.EqualError(t, err, "the runs replayed trace t1 and the baseline t2, so they cannot be compared")

	got, err = Compare(writeRuns(t, slices.Repeat([]string{`{"makespan_sec": 100}`}, 5)...), writeRuns(t, b...))
	require.NoError(t, err)
	require.Equal(t, -22.0, got["makespan_sec"].VsBaseline.Diff, "only one side is constant")
	require.Less(t, got["makespan_sec"].VsBaseline.CI95[0], -22.0)

	empty := t.TempDir()
	_, err = Compare([]string{empty}, writeRuns(t, b...))
	require.EqualError(t, err, "stat "+filepath.Join(empty, "results.json")+": no such file or directory")
	_, err = Compare(writeRuns(t, a...), []string{t.TempDir()})
	require.ErrorIs(t, err, os.ErrNotExist)
	notFile := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(notFile, "results.json"), 0o755))
	_, err = Compare(writeRuns(t, a...), []string{notFile})
	require.ErrorContains(t, err, "is a directory")
	bad := writeRuns(t, `{"makespan_sec":`)
	_, err = Compare(writeRuns(t, a...), bad)
	require.ErrorContains(t, err, bad[0]+": unexpected end of JSON input")
}
