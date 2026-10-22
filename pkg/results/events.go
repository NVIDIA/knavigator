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
	"encoding/csv"
	"os"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// maxEvents bounds memory when a scheduler floods warnings; raise it if runs need more evidence.
const maxEvents = 10000

// Event is a cluster event kept as evidence of a scheduler decision.
type Event struct {
	Time      time.Time
	Namespace string
	Kind      string
	Name      string
	Type      string
	Reason    string
	Message   string
	Count     int32
}

// isPreemption reports whether the reason marks a pod removed to make room for other work.
// "Preempted" comes from kube-scheduler and Volcano, "Evict" from KAI.
func isPreemption(reason string) bool {
	return reason == "Preempted" || reason == "Evict"
}

// keepEvent selects warnings and every eviction or preemption reason.
func keepEvent(e *corev1.Event) bool {
	if e.Type == corev1.EventTypeWarning {
		return true
	}
	switch e.Reason {
	case "Preempted", "Preempting", "Evict", "Evicted":
		return true
	}
	return false
}

func toEvent(e *corev1.Event) Event {
	t, count := e.LastTimestamp.Time, e.Count
	// events.k8s.io/v1 keeps repeats in Series and may leave the legacy fields zero.
	if e.Series != nil {
		t, count = e.Series.LastObservedTime.Time, e.Series.Count
	}
	if t.IsZero() {
		t = e.EventTime.Time
	}
	if t.IsZero() {
		t = e.CreationTimestamp.Time
	}
	if count == 0 {
		count = 1
	}
	return Event{Time: t, Namespace: e.InvolvedObject.Namespace, Kind: e.InvolvedObject.Kind,
		Name: e.InvolvedObject.Name, Type: e.Type, Reason: e.Reason, Message: e.Message, Count: count}
}

// preemptionEvents counts victim-side preemption events, one per removed pod.
func preemptionEvents(events []Event) int {
	var n int
	for _, e := range events {
		if isPreemption(e.Reason) {
			n += int(e.Count)
		}
	}
	return n
}

func writeEvents(path string, events []Event) error {
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	w := csv.NewWriter(f)
	_ = w.Write([]string{"time", "namespace", "kind", "name", "type", "reason", "count", "message"})
	for _, e := range events {
		_ = w.Write([]string{e.Time.UTC().Format(time.RFC3339Nano), e.Namespace, e.Kind, e.Name, e.Type,
			e.Reason, strconv.Itoa(int(e.Count)), e.Message})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return f.Close()
}
