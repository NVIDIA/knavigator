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
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	log "k8s.io/klog/v2"
)

// Only pods requesting this resource are recorded; make it a flag if other accelerators matter.
const gpuResource corev1.ResourceName = "nvidia.com/gpu"

// A variable so tests can poll quickly.
var pollInterval = 5 * time.Second

// Recorder watches GPU pods created after it starts.
type Recorder struct {
	client kubernetes.Interface
	start  time.Time
	stop   chan struct{}
	mutex  sync.Mutex
	pods   map[types.UID]*Pod
	pusher *Pusher
	events map[types.UID]*Event
	// Occurrences an event had before the run, left out of its count during the run.
	eventsBefore map[types.UID]int32
}

// Start begins watching pods and returns once the watch cache has synced.
func Start(ctx context.Context, client kubernetes.Interface) (*Recorder, error) {
	r := &Recorder{
		client: client,
		// Creation timestamps have one second resolution.
		start:  time.Now().Truncate(time.Second),
		stop:   make(chan struct{}),
		pods:   make(map[types.UID]*Pod),
		events: make(map[types.UID]*Event),

		eventsBefore: make(map[types.UID]int32),
	}

	factory := informers.NewSharedInformerFactory(client, 0)
	informer := factory.Core().V1().Pods().Informer()
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { r.observe(obj, false) },
		UpdateFunc: func(_, obj interface{}) { r.observe(obj, false) },
		DeleteFunc: func(obj interface{}) {
			if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = d.Obj
			}
			r.observe(obj, true)
		},
	})
	if err != nil {
		return nil, err
	}

	eventInformer := factory.Core().V1().Events().Informer()
	_, err = eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    r.observeEvent,
		UpdateFunc: func(_, obj interface{}) { r.observeEvent(obj) },
	})
	if err != nil {
		return nil, err
	}

	factory.Start(r.stop)
	if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced, eventInformer.HasSynced) {
		close(r.stop)
		return nil, fmt.Errorf("informers failed to sync")
	}
	return r, nil
}

// PushTo sends metrics to a Pushgateway every interval and again when the run ends. Besides the
// pod gauges, it pushes the summary of the run so far, using the jobs submitted up to that point.
func (r *Recorder) PushTo(ctx context.Context, url, run string, interval time.Duration, jobs func() []Job, speedup float64) {
	r.pusher = NewPusher(url, run)
	r.pusher.SetSpeedup(speedup)
	r.pusher.Start(r.snapshot, func(pods []Pod) (Summary, error) {
		capacity, maxNode, err := r.gpuCapacity(ctx)
		if err != nil {
			return Summary{}, err
		}
		floor := DefaultBSLDFloor
		if speedup > 1 {
			floor = time.Duration(float64(floor) / speedup)
		}
		s := Summarize(pods, jobs(), capacity, time.Now(), Options{MaxNodeGPUs: maxNode, BSLDFloor: floor})
		if speedup > 1 {
			s.ScaleTime(speedup)
		}
		return s, nil
	}, interval)
}

func (r *Recorder) observeEvent(obj interface{}) {
	e, ok := obj.(*corev1.Event)
	if !ok || !keepEvent(e) {
		return
	}
	ev := toEvent(e)

	r.mutex.Lock()
	defer r.mutex.Unlock()
	if ev.Time.Before(r.start) {
		r.eventsBefore[e.UID] = ev.Count
		return
	}
	ev.Count -= r.eventsBefore[e.UID]
	if _, seen := r.events[e.UID]; !seen && len(r.events) >= maxEvents {
		return
	}
	r.events[e.UID] = &ev
}

func (r *Recorder) eventList() []Event {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	events := make([]Event, 0, len(r.events))
	for _, e := range r.events {
		events = append(events, *e)
	}
	return events
}

func (r *Recorder) observe(obj interface{}, deleted bool) {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.CreationTimestamp.Time.Before(r.start) {
		return
	}
	gpus := podGPUs(pod)
	if gpus == 0 {
		return
	}
	now := time.Now()

	r.mutex.Lock()
	defer r.mutex.Unlock()

	p, ok := r.pods[pod.UID]
	if !ok {
		p = &Pod{Namespace: pod.Namespace, Name: pod.Name, GPUs: gpus, Created: pod.CreationTimestamp.Time}
		if pod.Spec.Priority != nil {
			p.Priority = int(*pod.Spec.Priority)
		}
		r.pods[pod.UID] = p
	}
	// A removed pod stays removed; later updates report the terminal phase it was given on the way out.
	if p.Phase != phaseDeleted {
		p.Phase = string(pod.Status.Phase)
	}
	if pod.Spec.NodeName != "" {
		p.Node = pod.Spec.NodeName
		if p.Scheduled.IsZero() {
			p.Scheduled = scheduledTime(pod, now)
		}
	}
	if p.Finished.IsZero() {
		switch {
		case pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed:
			p.Finished = finishedTime(pod, now)
			// Since Kubernetes 1.27 a deleted or preempted pod first turns Failed, then disappears.
			if pod.Status.Phase == corev1.PodFailed && removed(pod) {
				p.Phase = phaseDeleted
			}
		case deleted:
			p.Finished = now
			p.Phase = phaseDeleted
		}
	}
}

func podGPUs(pod *corev1.Pod) float64 {
	var n float64
	for _, c := range pod.Spec.Containers {
		if q, ok := c.Resources.Requests[gpuResource]; ok {
			n += q.AsApproximateFloat64()
		}
	}
	return n
}

// removed reports whether the pod is being deleted or was disrupted by preemption or eviction.
func removed(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return true
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.DisruptionTarget && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func scheduledTime(pod *corev1.Pod, fallback time.Time) time.Time {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionTrue {
			return c.LastTransitionTime.Time
		}
	}
	return fallback
}

func finishedTime(pod *corev1.Pod, fallback time.Time) time.Time {
	var t time.Time
	for _, s := range pod.Status.ContainerStatuses {
		if s.State.Terminated != nil {
			t = latest(t, s.State.Terminated.FinishedAt.Time)
		}
	}
	if t.IsZero() {
		return fallback
	}
	return t
}

func (r *Recorder) snapshot() []Pod {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	pods := make([]Pod, 0, len(r.pods))
	for _, p := range r.pods {
		pods = append(pods, *p)
	}
	sort.Slice(pods, func(i, j int) bool {
		if !pods[i].Created.Equal(pods[j].Created) {
			return pods[i].Created.Before(pods[j].Created)
		}
		return pods[i].Name < pods[j].Name
	})
	return pods
}

// idle reports whether every job has placed and finished its pods and no recorded pod is still active.
func idle(pods []Pod, jobs []Job) bool {
	assignJobs(pods, jobs)
	seen := map[string]int{}
	for _, p := range pods {
		if p.Finished.IsZero() {
			return false
		}
		// An evicted pod does not finish its job: the controller creates a replacement. A pod that
		// ended without ever being placed does not either.
		if !p.Scheduled.IsZero() && p.Phase != phaseDeleted {
			seen[p.Job]++
		}
	}
	for _, j := range jobs {
		if seen[j.Name] < j.Pods {
			return false
		}
	}
	return true
}

// Finish waits until the run drains or the timeout passes, then writes the results into dir.
func (r *Recorder) Finish(ctx context.Context, dir string, timeout time.Duration, jobs []Job, speedup float64, stall, bsldFloor time.Duration) error {
	defer close(r.stop)
	if r.pusher != nil {
		defer r.pusher.Stop()
	}

	deadline := time.Now().Add(timeout)
	drained, stalled := true, false
	lastProgress, lastCount := time.Now(), -1
	// Two idle polls in a row give controllers time to create the next pods.
	for idleCount := 0; idleCount < 2; {
		if time.Now().After(deadline) {
			log.Warningf("Results: run did not drain within %s; recording pods as they are", timeout)
			drained = false
			break
		}
		pods := r.snapshot()
		if idle(pods, jobs) {
			idleCount++
		} else {
			idleCount = 0
		}
		if n := progress(pods); n != lastCount {
			lastCount, lastProgress = n, time.Now()
		} else if stall > 0 && idleCount == 0 && time.Since(lastProgress) > stall {
			log.Warningf("Results: no pod was placed or finished for %s; stopping the run and recording pods as they are", stall)
			drained, stalled = false, true
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	end := time.Now()

	capacity, maxNode, err := r.gpuCapacity(ctx)
	if err != nil {
		return err
	}
	pods := r.snapshot()
	events := r.eventList()
	if speedup > 1 {
		// Runtimes are measured in real time; the floor is given in simulated time.
		bsldFloor = time.Duration(float64(bsldFloor) / speedup)
	}
	summary := Summarize(pods, jobs, capacity, end, Options{MaxNodeGPUs: maxNode, BSLDFloor: bsldFloor})
	summary.PreemptionEvents = preemptionEvents(events)
	summary.Stalled = stalled
	if speedup > 1 {
		summary.ScaleTime(speedup)
	}
	if r.pusher != nil {
		r.pusher.Stop()
		if err := r.pusher.Final(ctx, summary, pods); err != nil {
			log.Warningf("Results: final push failed: %v", err)
		}
	}
	log.Infof("Results: %d pods (%d unscheduled), %d/%d jobs started, makespan %.0fs, GPU utilization %.1f%%, job wait p95 %.0fs",
		summary.Pods, summary.PodsUnscheduled, summary.JobsStarted, summary.Jobs,
		summary.MakespanSec, 100*summary.GPUUtilization, summary.JobWaitSec.P95)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "results.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := writeTimeline(filepath.Join(dir, "timeline.csv"), pods); err != nil {
		return err
	}
	if err := writeEvents(filepath.Join(dir, "events.csv"), events); err != nil {
		return err
	}
	return summary.check(drained)
}

// progress counts placements and completions; a run that stops changing it is not advancing.
func progress(pods []Pod) int {
	var n int
	for _, p := range pods {
		if !p.Scheduled.IsZero() {
			n++
		}
		if !p.Finished.IsZero() {
			n++
		}
	}
	return n
}

// gpuCapacity sums allocatable GPUs over all nodes at the end of the run and finds the biggest node.
func (r *Recorder) gpuCapacity(ctx context.Context) (total, maxNode float64, err error) {
	nodes, err := r.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list nodes: %v", err)
	}
	for _, node := range nodes.Items {
		if q, ok := node.Status.Allocatable[gpuResource]; ok {
			g := q.AsApproximateFloat64()
			total += g
			maxNode = math.Max(maxNode, g)
		}
	}
	return total, maxNode, nil
}

func writeTimeline(path string, pods []Pod) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	ts := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"namespace", "pod", "job", "node", "gpus", "priority", "phase", "created", "scheduled", "finished"})
	for _, p := range pods {
		_ = w.Write([]string{p.Namespace, p.Name, p.Job, p.Node, strconv.FormatFloat(p.GPUs, 'f', -1, 64),
			strconv.Itoa(p.Priority), p.Phase, ts(p.Created), ts(p.Scheduled), ts(p.Finished)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return f.Close()
}
