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

package eventlog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)

func ev(seq int64, tr Transition) Event {
	return Event{
		Schema: 1, Seq: seq, RunID: "r1", ObservedAt: t0.Add(time.Duration(seq) * time.Second),
		Kind: "Pod", Namespace: "scs-gang", Name: "p", UID: "u",
		Probe: "gang", Job: "gang-a", Tenant: "t1", GPUs: 8, Transition: tr,
	}
}

// line1 is ev(1, Created) as the suite's recorder writes it.
const line1 = `{"schema":1,"seq":1,"run_id":"r1","observed_at":"2026-10-10T10:00:01Z","kind":"Pod","namespace":"scs-gang","name":"p","uid":"u","probe":"gang","job":"gang-a","tenant":"t1","priority":0,"gpus":8,"transition":"created"}`

// seq2 returns line1 renumbered as seq 2, observed at the same instant.
func seq2(line string) string {
	return strings.Replace(line, `"seq":1`, `"seq":2`, 1)
}

func TestEncodeAndRead(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	apiTime := time.Date(2026, 10, 10, 10, 0, 1, 0, time.UTC)
	second := ev(2, Scheduled)
	second.APITime = &apiTime
	second.APITimePrecision = "s"
	second.Node = "n1"
	require.NoError(t, enc.Encode(ev(1, Created)))
	require.NoError(t, enc.Encode(second))
	require.NoError(t, enc.Flush())
	require.Equal(t, line1+"\n"+
		`{"schema":1,"seq":2,"run_id":"r1","observed_at":"2026-10-10T10:00:02Z","api_time":"2026-10-10T10:00:01Z","api_time_precision":"s","kind":"Pod","namespace":"scs-gang","name":"p","uid":"u","probe":"gang","job":"gang-a","tenant":"t1","priority":0,"gpus":8,"transition":"scheduled","node":"n1"}`+"\n",
		buf.String())

	got, err := Read(&buf)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, Created, got[0].Transition)
	require.Equal(t, "n1", got[1].Node)
	require.True(t, got[1].APITime.Equal(apiTime))
	require.True(t, got[1].ObservedAt.Equal(t0.Add(2*time.Second)))
}

func TestTransitionValues(t *testing.T) {
	// the values are the suite's; a renamed one would break reading its logs
	want := map[Transition]string{
		Created:       "created",
		Scheduled:     "scheduled",
		Running:       "running",
		Succeeded:     "succeeded",
		Failed:        "failed",
		Deleted:       "deleted",
		Preempted:     "preempted",
		Evicted:       "evicted",
		Admitted:      "admitted",
		QuotaReserved: "quota_reserved",
		Requeued:      "requeued",
		Unschedulable: "unschedulable",
	}
	require.Len(t, want, 12)
	for tr, s := range want {
		require.Equal(t, s, string(tr))
		require.NoError(t, ev(1, tr).Validate(), "transition %s", s)
	}
}

func TestValidate(t *testing.T) {
	apiTime := t0
	testCases := []struct {
		name   string
		modify func(e *Event)
		err    string
	}{
		{
			name:   "valid",
			modify: func(e *Event) {},
		},
		{
			name:   "api_time with second precision",
			modify: func(e *Event) { e.APITime, e.APITimePrecision = &apiTime, "s" },
		},
		{
			name:   "api_time with microsecond precision",
			modify: func(e *Event) { e.APITime, e.APITimePrecision = &apiTime, "us" },
		},
		{
			name:   "schema 2",
			modify: func(e *Event) { e.Schema = 2 },
			err:    "seq 1: schema 2, want 1",
		},
		{
			name:   "schema missing",
			modify: func(e *Event) { e.Schema = 0 },
			err:    "seq 1: schema 0, want 1",
		},
		{
			name:   "seq 0",
			modify: func(e *Event) { e.Seq = 0 },
			err:    "seq 0: seq must be 1 or more",
		},
		{
			name:   "negative seq",
			modify: func(e *Event) { e.Seq = -1 },
			err:    "seq -1: seq must be 1 or more",
		},
		{
			name:   "observed_at missing",
			modify: func(e *Event) { e.ObservedAt = time.Time{} },
			err:    "seq 1: observed_at is missing",
		},
		{
			name:   "kind missing",
			modify: func(e *Event) { e.Kind = "" },
			err:    "seq 1: kind is missing",
		},
		{
			name:   "unknown transition",
			modify: func(e *Event) { e.Transition = "teleported" },
			err:    `seq 1: unknown transition "teleported"`,
		},
		{
			name:   "transition missing",
			modify: func(e *Event) { e.Transition = "" },
			err:    `seq 1: unknown transition ""`,
		},
		{
			name:   "api_time without precision",
			modify: func(e *Event) { e.APITime = &apiTime },
			err:    `seq 1: api_time_precision "", want s or us`,
		},
		{
			name:   "api_time with millisecond precision",
			modify: func(e *Event) { e.APITime, e.APITimePrecision = &apiTime, "ms" },
			err:    `seq 1: api_time_precision "ms", want s or us`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := ev(1, Created)
			tc.modify(&e)
			err := e.Validate()
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.err)
			}
		})
	}
}

func TestEncodeRefusesInvalidEvent(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	require.EqualError(t, enc.Encode(ev(0, Created)), "seq 0: seq must be 1 or more")
	require.NoError(t, enc.Flush())
	require.Empty(t, buf.String())
}

func TestEncodeMarshalError(t *testing.T) {
	e := ev(1, Created)
	e.ObservedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	require.EqualError(t, enc.Encode(e),
		"seq 1: json: error calling MarshalJSON for type *time.Time: year outside of range [0,9999]")
	require.NoError(t, enc.Flush())
	require.Empty(t, buf.String())
}

func TestEncodeWritesWholeLines(t *testing.T) {
	var log writeLog
	enc := NewEncoder(&log)
	// each line is about 220 bytes (line1 is 217), so 100 lines are about
	// 22000 bytes: the 4096-byte buffer fills at least 5 times with no Flush
	for seq := int64(1); seq <= 100; seq++ {
		require.NoError(t, enc.Encode(ev(seq, Created)))
	}
	require.GreaterOrEqual(t, len(log.writes), 5)
	for i, w := range log.writes {
		require.True(t, bytes.HasSuffix(w, []byte("\n")), "write %d", i)
	}
	require.NoError(t, enc.Flush())
	var all []byte
	for i, w := range log.writes {
		require.True(t, bytes.HasSuffix(w, []byte("\n")), "write %d", i)
		all = append(all, w...)
	}
	got, err := Read(bytes.NewReader(all))
	require.NoError(t, err)
	require.Len(t, got, 100)
}

func TestEncodeWriteError(t *testing.T) {
	enc := NewEncoder(failWriter{})
	// a short line stays in the buffer, so the error surfaces at Flush
	require.NoError(t, enc.Encode(ev(1, Created)))
	require.EqualError(t, enc.Flush(), "disk full")
}

func TestRead(t *testing.T) {
	falling := strings.Replace(seq2(line1),
		`"observed_at":"2026-10-10T10:00:01Z"`, `"observed_at":"2026-10-10T10:00:00.999Z"`, 1)
	testCases := []struct {
		name string
		in   string
		n    int
		err  string
	}{
		{
			name: "empty input",
			in:   "",
			n:    0,
		},
		{
			name: "two events observed at the same instant",
			in:   line1 + "\n" + seq2(line1) + "\n",
			n:    2,
		},
		{
			name: "last line without a newline",
			in:   line1 + "\n" + seq2(line1),
			n:    2,
		},
		{
			name: "spaces after the event",
			in:   line1 + "  \t\n",
			n:    1,
		},
		{
			name: "gap in seq",
			in:   line1 + "\n" + strings.Replace(line1, `"seq":1`, `"seq":3`, 1) + "\n",
			err:  "line 2: seq 3, want 2",
		},
		{
			name: "first seq is not 1",
			in:   seq2(line1) + "\n",
			err:  "line 1: seq 2, want 1",
		},
		{
			name: "observed_at falls while seq rises",
			in:   line1 + "\n" + falling + "\n",
			err:  "line 2: observed_at 2026-10-10T10:00:00.999Z is before line 1's 2026-10-10T10:00:01Z",
		},
		{
			name: "truncated last line",
			in:   line1 + "\n" + `{"schema":1,"seq":2,"run_id":"r1",` + "\n",
			err:  "line 2: unexpected EOF",
		},
		{
			name: "not JSON",
			in:   "garbage\n",
			err:  "line 1: invalid character 'g' looking for beginning of value",
		},
		{
			name: "blank line",
			in:   line1 + "\n\n",
			err:  "line 2: EOF",
		},
		{
			name: "two events joined on one line",
			in:   line1 + seq2(line1) + "\n",
			err:  "line 1: unexpected data after the event",
		},
		{
			name: "field the schema does not define",
			in:   strings.Replace(line1, `"kind":"Pod"`, `"kind":"Pod","colour":"red"`, 1),
			err:  `line 1: json: unknown field "colour"`,
		},
		{
			name: "unknown transition",
			in:   strings.Replace(line1, `"created"`, `"teleported"`, 1),
			err:  `line 1: seq 1: unknown transition "teleported"`,
		},
		{
			name: "schema other than 1",
			in:   strings.Replace(line1, `"schema":1`, `"schema":2`, 1),
			err:  "line 1: seq 1: schema 2, want 1",
		},
		{
			name: "observed_at missing",
			in:   strings.Replace(line1, `"observed_at":"2026-10-10T10:00:01Z",`, "", 1),
			err:  "line 1: seq 1: observed_at is missing",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Read(strings.NewReader(tc.in))
			if tc.err == "" {
				require.NoError(t, err)
				require.Len(t, got, tc.n)
			} else {
				require.EqualError(t, err, tc.err)
				require.Nil(t, got)
			}
		})
	}
}

func TestReadLineLimit(t *testing.T) {
	// a line may be up to 1 MiB, well past bufio.Scanner's 64 KiB default
	long := func(seq int64, size int) string {
		t.Helper()
		e := ev(seq, Created)
		e.Message = strings.Repeat("x", size)
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		require.NoError(t, enc.Encode(e))
		require.NoError(t, enc.Flush())
		return buf.String()
	}

	got, err := Read(strings.NewReader(line1 + "\n" + long(2, 1<<20-1024)))
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Len(t, got[1].Message, 1<<20-1024)

	_, err = Read(strings.NewReader(line1 + "\n" + long(2, 1<<20+1024)))
	require.EqualError(t, err, "line 2: bufio.Scanner: token too long")
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good.jsonl")
	require.NoError(t, os.WriteFile(good, []byte(line1+"\n"+seq2(line1)+"\n"), 0o644))
	got, err := ReadFile(good)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, int64(2), got[1].Seq)

	bad := filepath.Join(dir, "bad.jsonl")
	require.NoError(t, os.WriteFile(bad, []byte(line1+"\n"+line1+"\n"), 0o644))
	_, err = ReadFile(bad)
	require.EqualError(t, err, bad+": line 2: seq 1, want 2")

	missing := filepath.Join(dir, "missing.jsonl")
	_, err = ReadFile(missing)
	require.EqualError(t, err, "open "+missing+": no such file or directory")
}

// writeLog keeps a copy of every Write call made to it.
type writeLog struct{ writes [][]byte }

func (w *writeLog) Write(p []byte) (int, error) {
	w.writes = append(w.writes, append([]byte(nil), p...))
	return len(p), nil
}

// failWriter fails every write.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }
