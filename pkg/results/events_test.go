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
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKeepEvent(t *testing.T) {
	require.True(t, keepEvent(&corev1.Event{Type: corev1.EventTypeWarning, Reason: "FailedScheduling"}))
	require.True(t, keepEvent(&corev1.Event{Type: corev1.EventTypeNormal, Reason: "Evict"}))
	require.True(t, keepEvent(&corev1.Event{Type: corev1.EventTypeNormal, Reason: "Preempted"}))
	require.False(t, keepEvent(&corev1.Event{Type: corev1.EventTypeNormal, Reason: "Scheduled"}))
}

func TestPreemptionEvents(t *testing.T) {
	events := []Event{
		{Reason: "Evict", Count: 1},
		{Reason: "Preempted", Count: 3},
		// The preemptor's own event and kubelet evictions are not victims of the scheduler.
		{Reason: "Preempting", Count: 1},
		{Reason: "Evicted", Count: 1},
		{Reason: "FailedScheduling", Count: 50},
	}
	require.Equal(t, 4, preemptionEvents(events))
}

func TestToEventTimeFallback(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 5, 0, time.UTC)
	e := toEvent(&corev1.Event{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(created)}, Reason: "Evict"})
	require.Equal(t, created, e.Time)
	require.Equal(t, int32(1), e.Count)

	last := created.Add(time.Minute)
	e = toEvent(&corev1.Event{LastTimestamp: metav1.NewTime(last), Count: 7})
	require.Equal(t, last, e.Time)
	require.Equal(t, int32(7), e.Count)
}

func TestWriteEventsSorted(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "events.csv")
	require.NoError(t, writeEvents(path, []Event{
		{Time: t0.Add(time.Second), Reason: "Evict", Message: "second, with comma"},
		{Time: t0, Reason: "FailedScheduling", Message: "first"},
	}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "time,namespace,kind,name,type,reason,count,message\n"+
		"2026-01-01T00:00:00Z,,,,,FailedScheduling,0,first\n"+
		"2026-01-01T00:00:01Z,,,,,Evict,0,\"second, with comma\"\n", string(data))
}
