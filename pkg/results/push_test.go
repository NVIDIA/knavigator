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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/require"
)

// values flattens samples into "name{labels}" -> value.
func values(ss []sample) map[string]float64 {
	out := map[string]float64{}
	for _, s := range ss {
		key := s.name
		for _, l := range []string{"bucket", "gang_size", "priority", "stat"} {
			if v, ok := s.labels[l]; ok {
				key += "," + l + "=" + v
			}
		}
		out[key] = s.value
	}
	return out
}

func TestSamples(t *testing.T) {
	var tree interface{}
	require.NoError(t, json.Unmarshal([]byte(`{
		"pods": 4,
		"speedup": 60,
		"stalled": true,
		"gpu_utilization": 0.5,
		"note": "text is skipped",
		"missing": null,
		"job_wait_sec": {"count": 2, "mean": 3, "p95": 5, "max": 5},
		"evictions_by_priority": [
			{"priority": 50, "pods": 4, "jobs": 2, "gpu_hours": 12.5}
		],
		"jobs_by_gang_size": [
			{"gang_size": 8, "submitted": 3, "waiting": 1, "wait_sec": {"count": 2, "mean": 4, "p95": 6, "max": 6}}
		],
		"wait_by_footprint": [
			{"bucket": "1", "jobs": 7, "wait_sec": {"count": 1, "mean": 2, "p95": 2, "max": 2}},
			{"bucket": "17+", "jobs": 1, "wait_sec": {"count": 0, "mean": 0, "p95": 0, "max": 0}}
		]
	}`), &tree))

	got := values(samples(tree))
	require.NotContains(t, got, "knavigator_speedup", "the pusher publishes the speedup itself")

	require.Equal(t, 4.0, got["knavigator_pods"])
	require.Equal(t, 1.0, got["knavigator_stalled"])
	require.Equal(t, 0.5, got["knavigator_gpu_utilization"])
	require.Equal(t, 5.0, got["knavigator_job_wait_sec,stat=p95"])
	require.Equal(t, 7.0, got["knavigator_wait_by_footprint_jobs,bucket=1"])
	require.Equal(t, 1.0, got["knavigator_wait_by_footprint_jobs,bucket=17+"])
	require.Equal(t, 2.0, got["knavigator_wait_by_footprint_wait_sec,bucket=1,stat=mean"])
	require.Equal(t, 4.0, got["knavigator_evictions_by_priority_pods,priority=50"])
	require.Equal(t, 12.5, got["knavigator_evictions_by_priority_gpu_hours,priority=50"])
	require.Equal(t, 3.0, got["knavigator_jobs_by_gang_size_submitted,gang_size=8"])
	require.Equal(t, 1.0, got["knavigator_jobs_by_gang_size_waiting,gang_size=8"])
	require.Equal(t, 6.0, got["knavigator_jobs_by_gang_size_wait_sec,gang_size=8,stat=p95"])
	require.NotContains(t, got, "knavigator_note")
	require.NotContains(t, got, "knavigator_missing")
}

type pushed struct {
	mu       sync.Mutex
	path     string
	families map[string]*dto.MetricFamily
}

func (p *pushed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	dec := expfmt.NewDecoder(bytes.NewReader(data), expfmt.ResponseFormat(r.Header))
	families := map[string]*dto.MetricFamily{}
	for {
		var f dto.MetricFamily
		if dec.Decode(&f) != nil {
			break
		}
		families[f.GetName()] = &f
	}
	p.mu.Lock()
	p.path, p.families = r.URL.Path, families
	p.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// value returns the sample of a pushed metric whose labels match, or -1.
func (p *pushed) value(name string, labels map[string]string) float64 {
	f := p.families[name]
	if f == nil {
		return -1
	}
	for _, m := range f.GetMetric() {
		match := len(m.GetLabel()) == len(labels)
		for _, l := range m.GetLabel() {
			match = match && labels[l.GetName()] == l.GetValue()
		}
		if match {
			return m.GetGauge().GetValue()
		}
	}
	return -1
}

func TestPusher(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pods := []Pod{
		{GPUs: 8, Created: t0, Scheduled: t0}, // running
		{GPUs: 8, Created: t0},                // pending
		{GPUs: 8, Created: t0, Scheduled: t0, Finished: t0, Phase: "Succeeded"},  // done
		{GPUs: 4, Created: t0, Scheduled: t0, Finished: t0, Phase: phaseDeleted}, // evicted
		{GPUs: 4, Created: t0, Finished: t0, Phase: phaseDeleted},                // deleted before placement
	}
	srv := &pushed{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	p := NewPusher(ts.URL, "r1")

	require.NoError(t, p.Push(context.Background(), pods))
	srv.mu.Lock()
	require.Equal(t, "/metrics/job/knavigator/run/r1", srv.path)
	require.Equal(t, 8.0, srv.value("knavigator_gpus_in_use", nil))
	require.Equal(t, 8.0, srv.value("knavigator_gpus_pending", nil))
	for state, n := range map[string]float64{"running": 1, "pending": 1, "succeeded": 1, "evicted": 1, "deleted": 1, "failed": 0} {
		require.Equal(t, n, srv.value("knavigator_pods_by_state", map[string]string{"state": state}), state)
	}
	srv.mu.Unlock()

	require.Equal(t, 1.0, srv.value("knavigator_speedup", nil), "no speedup reads as 1")
	p.SetSpeedup(60)
	require.NoError(t, p.Push(context.Background(), pods))
	require.Equal(t, 60.0, srv.value("knavigator_speedup", nil))

	s := Summary{Pods: 5, GPUUtilization: 0.75, JobWaitSec: Stats{Count: 1, Mean: 2, P95: 3, Max: 3}}
	require.NoError(t, p.Final(context.Background(), s, pods))
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Equal(t, 0.75, srv.value("knavigator_gpu_utilization", nil))
	require.Equal(t, 3.0, srv.value("knavigator_job_wait_sec", map[string]string{"stat": "p95"}))
	require.Equal(t, 8.0, srv.value("knavigator_gpus_in_use", nil), "the final push keeps the live gauges")
}

func TestPusherStartStop(t *testing.T) {
	srv := &pushed{}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	p := NewPusher(ts.URL, "r2")

	p.Start(func() []Pod { return []Pod{{GPUs: 2, Scheduled: time.Now()}} },
		func([]Pod) (Summary, error) { return Summary{GPUUtilization: 0.25}, nil }, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.path == "/metrics/job/knavigator/run/r2" && srv.value("knavigator_gpu_utilization", nil) == 0.25
	}, 5*time.Second, 10*time.Millisecond)
	p.Stop()
	p.Stop() // safe to call twice
}
