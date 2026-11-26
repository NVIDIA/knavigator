/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
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
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	log "k8s.io/klog/v2"
)

// expiredWatch is a recorder on a fake clientset whose first watch of resource ends with 410 Expired
// once release is called. The reflector then relists without calling the watch error handler.
type expiredWatch struct {
	rec     *Recorder
	release func()
	// lists counts the lists of resource since Start returned.
	lists *atomic.Int32
	// failNextList makes the next list of resource fail.
	failNextList *atomic.Bool
}

func startWithExpiredWatch(t *testing.T, resource string) expiredWatch {
	t.Helper()
	client := fake.NewSimpleClientset()
	released := make(chan struct{})
	var lists, watches atomic.Int32
	var failNext atomic.Bool
	client.PrependReactor("list", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		lists.Add(1)
		if failNext.CompareAndSwap(true, false) {
			return true, nil, errors.New("injected list failure")
		}
		return false, nil, nil
	})
	client.PrependWatchReactor(resource, func(k8stesting.Action) (bool, watch.Interface, error) {
		if watches.Add(1) > 1 {
			return false, nil, nil
		}
		fw := watch.NewFakeWithChanSize(1, false)
		go func() {
			<-released
			fw.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 410, Reason: metav1.StatusReasonExpired, Message: "too old resource version"})
		}()
		return true, fw, nil
	})
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	lists.Store(0)
	var once atomic.Bool
	return expiredWatch{rec: rec, lists: &lists, failNextList: &failNext,
		release: func() {
			if once.CompareAndSwap(false, true) {
				close(released)
			}
		}}
}

// Once Start has returned, a pod or event watch that ends with 410 Expired makes the real client-go
// reflector relist without calling the watch error handler, and the recorder counts exactly 1.
func TestWatchErrorsCountARelistAfterAnExpiredWatch(t *testing.T) {
	for _, resource := range []string{"pods", "events"} {
		t.Run(resource, func(t *testing.T) {
			w := startWithExpiredWatch(t, resource)
			defer close(w.rec.stop)
			require.Zero(t, w.rec.watch.count(), "the first sync is not a relist")

			w.release()

			require.Eventually(t, func() bool { return w.rec.watch.count() == 1 }, 10*time.Second, 10*time.Millisecond)
			require.EqualValues(t, 1, w.lists.Load(), "one relist")
			require.Never(t, func() bool { return w.rec.watch.count() != 1 }, 500*time.Millisecond, 20*time.Millisecond)
		})
	}
}

// A List that fails during Start's first sync is not counted, since the run has not started.
func TestWatchErrorsSkipAFailureBeforeTheFirstSync(t *testing.T) {
	client := fake.NewSimpleClientset()
	var failed atomic.Bool
	client.PrependReactor("list", "events", func(k8stesting.Action) (bool, runtime.Object, error) {
		if failed.CompareAndSwap(false, true) {
			return true, nil, errors.New("injected list failure")
		}
		return false, nil, nil
	})
	rec, err := Start(context.Background(), client, HarnessOptions{})
	require.NoError(t, err)
	defer close(rec.stop)

	require.True(t, failed.Load(), "the first sync saw the failure")
	require.Zero(t, rec.watch.count())
}

// After the 410, a relist whose List fails once counts 2: the watch error handler's call for the
// failure, then the relist that succeeds. The failure still reaches client-go's own log line, since
// the handler calls cache.DefaultWatchErrorHandler.
func TestWatchErrorsCountAFailedRelist(t *testing.T) {
	buf := captureLog(t)
	w := startWithExpiredWatch(t, "pods")
	defer close(w.rec.stop)
	w.failNextList.Store(true)

	w.release()

	require.Eventually(t, func() bool { return w.rec.watch.count() == 2 }, 20*time.Second, 10*time.Millisecond)
	log.Flush()
	require.Contains(t, buf.String(), "injected list failure")
}

// Finish records the count as watch_errors and warns, so a user who never opens manifest.json
// still learns that the recorder may have missed changes.
func TestFinishReportsWatchErrors(t *testing.T) {
	fastPoll(t)
	buf := captureLog(t)
	w := startWithExpiredWatch(t, "pods")
	w.release()
	require.Eventually(t, func() bool { return w.rec.watch.count() == 1 }, 10*time.Second, 10*time.Millisecond)

	dir := t.TempDir()
	require.NoError(t, w.rec.Finish(context.Background(), dir, time.Minute, nil, 1, 0, DefaultBSLDFloor))

	var m manifest
	readJSON(t, filepath.Join(dir, "manifest.json"), &m)
	require.Equal(t, 1, m.Profiles[UnknownProfile].WatchErrors)
	log.Flush()
	require.Contains(t, buf.String(), "the pod or event watch failed or relisted 1 times")
}

// A server that serves watch-list relists with a watch that asks for the initial events again. The
// real reflector, over a client without the opt-out, makes the first sync and the relist such
// watches, and the recorder counts the relist only.
func TestWatchErrorsCountAWatchListRelist(t *testing.T) {
	client, err := kubernetes.NewForConfig(&rest.Config{Host: "http://127.0.0.1:1"})
	require.NoError(t, err)
	var lists, initialWatches, plainWatches atomic.Int32
	released := make(chan struct{})
	lw := &cache.ListWatch{
		ListWithContextFunc: func(context.Context, metav1.ListOptions) (runtime.Object, error) {
			lists.Add(1)
			return &corev1.PodList{ListMeta: metav1.ListMeta{ResourceVersion: "1"}}, nil
		},
		WatchFuncWithContext: func(_ context.Context, o metav1.ListOptions) (watch.Interface, error) {
			fw := watch.NewFakeWithChanSize(4, false)
			if o.SendInitialEvents == nil || !*o.SendInitialEvents {
				plainWatches.Add(1)
				return fw, nil
			}
			n := initialWatches.Add(1)
			fw.Action(watch.Bookmark, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "5",
				Annotations: map[string]string{metav1.InitialEventsAnnotationKey: "true"}}})
			if n == 1 {
				go func() {
					<-released
					fw.Error(&metav1.Status{Status: metav1.StatusFailure, Code: 410, Reason: metav1.StatusReasonExpired, Message: "too old resource version"})
				}()
			}
			return fw, nil
		},
	}
	var w watchErrors
	inf, err := w.informer(client, &corev1.Pod{}, lw)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go inf.RunWithContext(ctx)
	require.Eventually(t, inf.HasSynced, 10*time.Second, 10*time.Millisecond)
	w.synced.Store(true)
	require.Zero(t, w.count())

	close(released)

	require.Eventually(t, func() bool { return initialWatches.Load() == 2 }, 10*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return w.count() == 1 }, 10*time.Second, 10*time.Millisecond)
	require.Zero(t, lists.Load())
	require.Zero(t, plainWatches.Load())
}

// countingListerWatcher counts a relist start and nothing else: nothing before the first sync, a
// List without a continue token or a watch with SendInitialEvents after it, and not a page after
// the first, a watch from a resource version, or a failed call.
func TestCountingListerWatcherCountsRelistStarts(t *testing.T) {
	var listErr, watchErr error
	stub := &cache.ListWatch{
		ListWithContextFunc: func(context.Context, metav1.ListOptions) (runtime.Object, error) {
			return &corev1.PodList{}, listErr
		},
		WatchFuncWithContext: func(context.Context, metav1.ListOptions) (watch.Interface, error) {
			return watch.NewFake(), watchErr
		},
	}
	var w watchErrors
	c := &countingListerWatcher{inner: stub, w: &w}
	ctx := context.Background()
	yes := true
	initial := metav1.ListOptions{SendInitialEvents: &yes}

	_, err := c.ListWithContext(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	_, err = c.WatchWithContext(ctx, initial)
	require.NoError(t, err)
	require.Zero(t, w.count(), "the first sync is not counted")

	w.synced.Store(true)
	// A relist of 1200 pods in pages of 500 is three Lists, and only the first has no token.
	_, err = c.ListWithContext(ctx, metav1.ListOptions{Limit: 500})
	require.NoError(t, err)
	require.Equal(t, 1, w.count())
	for _, token := range []string{"page2", "page3"} {
		_, err = c.ListWithContext(ctx, metav1.ListOptions{Limit: 500, Continue: token})
		require.NoError(t, err)
	}
	require.Equal(t, 1, w.count(), "the pages after the first are not relists")

	_, err = c.WatchWithContext(ctx, initial)
	require.NoError(t, err)
	require.Equal(t, 2, w.count(), "a watch that asks for the initial events again is a relist")
	_, err = c.WatchWithContext(ctx, metav1.ListOptions{ResourceVersion: "42"})
	require.NoError(t, err)
	_, err = c.WatchWithContext(ctx, metav1.ListOptions{SendInitialEvents: new(bool)})
	require.NoError(t, err)
	require.Equal(t, 2, w.count(), "a re-watch from a resource version is not a relist")

	listErr, watchErr = errors.New("list failed"), errors.New("watch failed")
	_, err = c.ListWithContext(ctx, metav1.ListOptions{})
	require.EqualError(t, err, "list failed")
	_, err = c.WatchWithContext(ctx, initial)
	require.EqualError(t, err, "watch failed")
	require.Equal(t, 2, w.count(), "the watch error handler counts a failed call")
}

// The wrapper reports a fake clientset's watch-list opt-out and a real clientset's lack of one, as
// watchlist.DoesClientNotSupportWatchListSemantics reads them. Without it the reflector would
// watch-list against the fake, and every Start test would hang.
func TestCountingListerWatcherForwardsTheWatchListOptOut(t *testing.T) {
	realClient, err := kubernetes.NewForConfig(&rest.Config{Host: "http://127.0.0.1:1"})
	require.NoError(t, err)
	wrap := func(client kubernetes.Interface) *countingListerWatcher {
		return &countingListerWatcher{inner: cache.ToListWatcherWithWatchListSemantics(&cache.ListWatch{}, client), w: &watchErrors{}}
	}
	require.True(t, wrap(fake.NewSimpleClientset()).IsWatchListSemanticsUnSupported())
	require.False(t, wrap(realClient).IsWatchListSemanticsUnSupported())
}
