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

// Package eventlog defines the run event log, schema version 1: one JSON
// object per line for each observed change to a Pod, Job, Event or Kueue
// Workload during a run. It is the events.jsonl format of the Scheduling
// Capabilities Suite, so either tool reads the other's logs.
package eventlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// SchemaVersion is the line format this package writes and accepts.
const SchemaVersion = 1

// Transition names what changed about an object.
type Transition string

// The transitions an event log records.
const (
	Created       Transition = "created"
	Scheduled     Transition = "scheduled"
	Running       Transition = "running"
	Succeeded     Transition = "succeeded"
	Failed        Transition = "failed"
	Deleted       Transition = "deleted"
	Preempted     Transition = "preempted"
	Evicted       Transition = "evicted"
	Admitted      Transition = "admitted"
	QuotaReserved Transition = "quota_reserved"
	Requeued      Transition = "requeued"
	Unschedulable Transition = "unschedulable"
)

var knownTransitions = map[Transition]bool{
	Created: true, Scheduled: true, Running: true, Succeeded: true, Failed: true,
	Deleted: true, Preempted: true, Evicted: true, Admitted: true,
	QuotaReserved: true, Requeued: true, Unschedulable: true,
}

// Event is one line of the event log. ObservedAt is when the recorder wrote
// the change, on its host clock; APITime is the object's own timestamp for
// it, when it has one, and APITimePrecision says whether that timestamp
// carries seconds ("s") or microseconds ("us"). Probe, Job and Tenant are
// optional labels a workflow sets on the objects it submits; Job is a
// label, not a Kubernetes Job's name.
type Event struct {
	Schema           int        `json:"schema"`
	Seq              int64      `json:"seq"`
	RunID            string     `json:"run_id"`
	ObservedAt       time.Time  `json:"observed_at"`
	APITime          *time.Time `json:"api_time,omitempty"`
	APITimePrecision string     `json:"api_time_precision,omitempty"`
	Kind             string     `json:"kind"`
	Namespace        string     `json:"namespace"`
	Name             string     `json:"name"`
	UID              string     `json:"uid"`
	Probe            string     `json:"probe,omitempty"`
	Job              string     `json:"job,omitempty"`
	Tenant           string     `json:"tenant,omitempty"`
	Priority         int32      `json:"priority"`
	GPUs             int64      `json:"gpus"`
	Transition       Transition `json:"transition"`
	Node             string     `json:"node,omitempty"`
	Reason           string     `json:"reason,omitempty"`
	Message          string     `json:"message,omitempty"`
}

// Validate returns the first way e breaks the schema, or nil.
func (e Event) Validate() error {
	if e.Schema != SchemaVersion {
		return fmt.Errorf("seq %d: schema %d, want %d", e.Seq, e.Schema, SchemaVersion)
	}
	if e.Seq < 1 {
		return fmt.Errorf("seq %d: seq must be 1 or more", e.Seq)
	}
	if e.ObservedAt.IsZero() {
		return fmt.Errorf("seq %d: observed_at is missing", e.Seq)
	}
	if e.Kind == "" {
		return fmt.Errorf("seq %d: kind is missing", e.Seq)
	}
	if !knownTransitions[e.Transition] {
		return fmt.Errorf("seq %d: unknown transition %q", e.Seq, e.Transition)
	}
	if e.APITime != nil && e.APITimePrecision != "s" && e.APITimePrecision != "us" {
		return fmt.Errorf("seq %d: api_time_precision %q, want s or us", e.Seq, e.APITimePrecision)
	}
	return nil
}

// Encoder writes events as JSON Lines. It is not safe for concurrent use.
type Encoder struct {
	w *bufio.Writer
}

// NewEncoder returns an Encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: bufio.NewWriter(w)}
}

// Encode validates e and writes it as one line.
func (enc *Encoder) Encode(e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("seq %d: %w", e.Seq, err)
	}
	line := append(b, '\n')
	// flush first when the line does not fit, so the underlying writer only
	// ever receives whole lines; an empty buffer writes a long line directly
	if len(line) > enc.w.Available() && enc.w.Buffered() > 0 {
		if err := enc.w.Flush(); err != nil {
			return err
		}
	}
	_, err = enc.w.Write(line)
	return err
}

// Flush writes any buffered lines to the underlying writer.
func (enc *Encoder) Flush() error {
	return enc.w.Flush()
}

const maxLineBytes = 1 << 20

// Read parses JSON Lines from r. Every line must be a valid Event, seq
// must run 1, 2, 3 and so on without a gap, and observed_at must never fall
// (a host clock that steps back would shift every duration).
func Read(r io.Reader) ([]Event, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	var out []Event
	line := 0
	for sc.Scan() {
		line++
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.DisallowUnknownFields()
		var e Event
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if len(bytes.TrimSpace(sc.Bytes()[dec.InputOffset():])) > 0 {
			return nil, fmt.Errorf("line %d: unexpected data after the event", line)
		}
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if want := int64(len(out) + 1); e.Seq != want {
			return nil, fmt.Errorf("line %d: seq %d, want %d", line, e.Seq, want)
		}
		if n := len(out); n > 0 && e.ObservedAt.Before(out[n-1].ObservedAt) {
			return nil, fmt.Errorf("line %d: observed_at %s is before line %d's %s",
				line, e.ObservedAt.Format(time.RFC3339Nano), line-1, out[n-1].ObservedAt.Format(time.RFC3339Nano))
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("line %d: %w", line+1, err)
	}
	return out, nil
}

// ReadFile reads and validates the event log at path.
func ReadFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	evs, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return evs, nil
}
