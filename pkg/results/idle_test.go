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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIdleWaitsForEvictedJob(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	jobs := []Job{{Name: "default/a", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "a-[a-z0-9]+"}}
	evicted := Pod{Namespace: "default", Name: "a-x1", Phase: phaseDeleted, Created: at(0), Scheduled: at(1), Finished: at(30)}

	// The pod was evicted and its replacement does not exist yet: the job is not done.
	require.False(t, idle([]Pod{evicted}, jobs))

	// The replacement is running, then finishes.
	running := Pod{Namespace: "default", Name: "a-x2", Phase: "Running", Created: at(30), Scheduled: at(31)}
	require.False(t, idle([]Pod{evicted, running}, jobs))
	done := running
	done.Phase, done.Finished = "Succeeded", at(100)
	require.True(t, idle([]Pod{evicted, done}, jobs))
}

func TestIdleIgnoresPodsNeverPlaced(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jobs := []Job{{Name: "default/a", Namespace: "default", Submitted: t0, Pods: 1, PodRegexp: "a-[a-z0-9]+"}}
	// A pod that failed before it was placed is not the job's finished pod.
	failed := Pod{Namespace: "default", Name: "a-x1", Phase: "Failed", Created: t0, Finished: t0.Add(5 * time.Second)}

	require.False(t, idle([]Pod{failed}, jobs))
}
