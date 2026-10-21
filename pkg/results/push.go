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
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	corev1 "k8s.io/api/core/v1"
	log "k8s.io/klog/v2"
)

const (
	pushJob     = "knavigator"
	metricsRoot = "knavigator_"
)

// Pusher sends run metrics to a Prometheus Pushgateway: live gauges while the run goes, then the
// summary. Every push replaces the group for this run, so repeated pushes never accumulate.
type Pusher struct {
	url, run string
	reg      *prometheus.Registry
	podState *prometheus.GaugeVec
	inUse    prometheus.Gauge
	pending  prometheus.Gauge
	speedup  prometheus.Gauge
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
	mutex    sync.Mutex
	vecs     map[string]*prometheus.GaugeVec
}

// NewPusher creates a pusher; run labels every series so separate runs stay apart.
func NewPusher(url, run string) *Pusher {
	p := &Pusher{
		url: url, run: run, reg: prometheus.NewRegistry(), vecs: map[string]*prometheus.GaugeVec{},
		podState: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: metricsRoot + "pods_by_state",
			Help: "GPU pods in each state: pending, running, succeeded, failed, evicted or deleted."}, []string{"state"}),
		inUse: prometheus.NewGauge(prometheus.GaugeOpts{Name: metricsRoot + "gpus_in_use",
			Help: "GPUs requested by running pods."}),
		pending: prometheus.NewGauge(prometheus.GaugeOpts{Name: metricsRoot + "gpus_pending",
			Help: "GPUs requested by pods not yet placed."}),
		speedup: prometheus.NewGauge(prometheus.GaugeOpts{Name: metricsRoot + "speedup",
			Help: "Factor by which the run compresses time; summary values already report simulated time."}),
	}
	p.speedup.Set(1)
	p.reg.MustRegister(p.podState, p.inUse, p.pending, p.speedup)
	return p
}

// SetSpeedup records the run's time compression, so dashboards can scale totals they compute from
// the live gauges, which are in real time.
func (p *Pusher) SetSpeedup(f float64) {
	if f > 1 {
		p.speedup.Set(f)
	}
}

// observe sets the live gauges from a pod snapshot.
func (p *Pusher) observe(pods []Pod) {
	states := map[string]float64{"pending": 0, "running": 0, "succeeded": 0, "failed": 0, "evicted": 0, "deleted": 0}
	var inUse, pending float64
	for _, pod := range pods {
		switch {
		case pod.Phase == phaseDeleted && !pod.Scheduled.IsZero():
			states["evicted"]++
		case pod.Phase == phaseDeleted:
			states["deleted"]++
		case pod.Phase == string(corev1.PodSucceeded):
			states["succeeded"]++
		case pod.Phase == string(corev1.PodFailed):
			states["failed"]++
		case pod.Scheduled.IsZero():
			states["pending"]++
			pending += pod.GPUs
		default:
			states["running"]++
			inUse += pod.GPUs
		}
	}
	for state, n := range states {
		p.podState.WithLabelValues(state).Set(n)
	}
	p.inUse.Set(inUse)
	p.pending.Set(pending)
}

// Push pushes the live gauges for the given pods.
func (p *Pusher) Push(ctx context.Context, pods []Pod) error {
	p.observe(pods)
	return p.send(ctx)
}

// Final sets the summary gauges and pushes once more.
func (p *Pusher) Final(ctx context.Context, s Summary, pods []Pod) error {
	p.observe(pods)
	p.setSummary(s)
	return p.send(ctx)
}

// setSummary sets one gauge per summary metric, creating each on first use.
func (p *Pusher) setSummary(s Summary) {
	data, err := json.Marshal(s)
	if err != nil {
		log.Warningf("Results: cannot encode the summary for pushing: %v", err)
		return
	}
	var tree interface{}
	if err = json.Unmarshal(data, &tree); err != nil {
		log.Warningf("Results: cannot decode the summary for pushing: %v", err)
		return
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for _, sm := range samples(tree) {
		vec, ok := p.vecs[sm.name]
		if !ok {
			names := make([]string, 0, len(sm.labels))
			for l := range sm.labels {
				names = append(names, l)
			}
			sort.Strings(names)
			vec = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: sm.name, Help: "Run result " + sm.name[len(metricsRoot):] + "."}, names)
			if err := p.reg.Register(vec); err != nil {
				log.Warningf("Results: skipping metric %s: %v", sm.name, err)
				continue
			}
			p.vecs[sm.name] = vec
		}
		vec.With(sm.labels).Set(sm.value)
	}
}

func (p *Pusher) send(ctx context.Context) error {
	return push.New(p.url, pushJob).Grouping("run", p.run).Gatherer(p.reg).PushContext(ctx)
}

// Start pushes every interval until Stop. The gauges come from the pod snapshot; when summarize is
// set, the summary of the run so far is pushed too, so those numbers are current during the run.
func (p *Pusher) Start(snapshot func() []Pod, summarize func([]Pod) (Summary, error), interval time.Duration) {
	p.stop, p.done = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(p.done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				pods := snapshot()
				p.observe(pods)
				if summarize != nil {
					if s, err := summarize(pods); err != nil {
						log.Warningf("Results: no summary to push yet: %v", err)
					} else {
						p.setSummary(s)
					}
				}
				if err := p.send(context.Background()); err != nil {
					log.Warningf("Results: push to %s failed: %v", p.url, err)
				}
			}
		}
	}()
}

// Stop ends the periodic pushes and waits for the last one to finish.
func (p *Pusher) Stop() {
	if p.stop != nil {
		p.once.Do(func() { close(p.stop) })
		<-p.done
	}
}

type sample struct {
	name   string
	labels prometheus.Labels
	value  float64
}

// samples turns a decoded results.json into one sample per metric value. Numbers become
// knavigator_<field>, booleans 0 or 1. An object of count, mean, p95 and max becomes the label
// stat. A list of objects with a bucket, gang_size or priority field becomes that label.
// Everything else is skipped.
func samples(tree interface{}) []sample {
	var out []sample
	add := func(name string, labels prometheus.Labels, v float64) {
		out = append(out, sample{name, labels, v})
	}
	var walk func(v interface{}, name string, labels prometheus.Labels)
	walk = func(v interface{}, name string, labels prometheus.Labels) {
		with := func(k, val string) prometheus.Labels {
			l := prometheus.Labels{k: val}
			for lk, lv := range labels {
				l[lk] = lv
			}
			return l
		}
		switch x := v.(type) {
		case float64:
			add(name, labels, x)
		case bool:
			f := 0.0
			if x {
				f = 1
			}
			add(name, labels, f)
		case map[string]interface{}:
			if isStats(x) {
				for stat, val := range x {
					if f, ok := val.(float64); ok {
						add(name, with("stat", stat), f)
					}
				}
				return
			}
			for k, val := range x {
				walk(val, name+"_"+k, labels)
			}
		case []interface{}:
			for _, item := range x {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				// The label comes from the field that names the row.
				key, id := "", ""
				if b, ok := m["bucket"].(string); ok {
					key, id = "bucket", b
				} else if n, ok := m["gang_size"].(float64); ok {
					key, id = "gang_size", strconv.Itoa(int(n))
				} else if n, ok := m["priority"].(float64); ok {
					key, id = "priority", strconv.Itoa(int(n))
				} else {
					continue
				}
				for k, val := range m {
					if k != key {
						walk(val, name+"_"+k, with(key, id))
					}
				}
			}
		}
	}
	m, _ := tree.(map[string]interface{})
	for k, v := range m {
		// knavigator_speedup is the pusher's own gauge, set whether or not the summary scaled.
		if k == "speedup" {
			continue
		}
		walk(v, metricsRoot+k, nil)
	}
	return out
}

// isStats recognizes the Stats object by its keys.
func isStats(m map[string]interface{}) bool {
	_, hasMean := m["mean"]
	_, hasP95 := m["p95"]
	return hasMean && hasP95
}
