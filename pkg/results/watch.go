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
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/watchlist"
)

// watchErrors counts the moments the recorder may have missed a pod or event change, for
// watch_errors in manifest.json: every call of an informer's watch error handler, and every relist,
// after Start's first sync. client-go relists after a 410 Expired watch without calling the watch
// error handler, so the ListerWatcher under each informer is the only place that sees every relist.
// The Scheduling Capabilities Suite counts the same two things (pkg/recorder/relist.go), except
// that it counts each page of a paged relist, where this counts the relist once.
//
// Start builds its informers through informer instead of an informer factory, because a factory
// builds its own ListerWatcher and gives no way to wrap it.
type watchErrors struct {
	// synced turns true once Start's informers have synced; a list before then is the first sync.
	synced atomic.Bool
	n      atomic.Int64
}

// informer builds an informer of example's type over lw, counting its watch errors and relists in w.
// client is the clientset behind lw. A fake clientset does not support watch-list, and the reflector
// must see that through the wrapper, or the first sync never ends.
func (w *watchErrors) informer(client kubernetes.Interface, example runtime.Object, lw *cache.ListWatch) (cache.SharedIndexInformer, error) {
	inner := cache.ToListWatcherWithWatchListSemantics(lw, client)
	inf := cache.NewSharedIndexInformer(&countingListerWatcher{inner: inner, w: w}, example, 0, cache.Indexers{})
	// A custom handler replaces client-go's log line, so it calls the default one.
	err := inf.SetWatchErrorHandlerWithContext(func(ctx context.Context, r *cache.Reflector, err error) {
		if w.synced.Load() {
			w.n.Add(1)
		}
		cache.DefaultWatchErrorHandler(ctx, r, err)
	})
	return inf, err
}

// count is the number of watch errors and relists so far.
func (w *watchErrors) count() int {
	return int(w.n.Load())
}

// countingListerWatcher is an informer's ListerWatcher that adds each relist after the first sync
// to w. A relist starts with a List without a continue token, since a paged List asks for each
// further page with one, or with a watch that asks for the initial events again, which is how a
// watch-list client relists. Failed calls are left to the watch error handler.
type countingListerWatcher struct {
	inner cache.ListerWatcher
	w     *watchErrors
}

// ListWithContext counts a successful List without a continue token, once synced.
func (c *countingListerWatcher) ListWithContext(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
	obj, err := cache.ToListerWatcherWithContext(c.inner).ListWithContext(ctx, opts)
	if err == nil && opts.Continue == "" && c.w.synced.Load() {
		c.w.n.Add(1)
	}
	return obj, err
}

// WatchWithContext counts a successful watch with opts.SendInitialEvents true, once synced.
func (c *countingListerWatcher) WatchWithContext(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	wi, err := cache.ToListerWatcherWithContext(c.inner).WatchWithContext(ctx, opts)
	if err == nil && opts.SendInitialEvents != nil && *opts.SendInitialEvents && c.w.synced.Load() {
		c.w.n.Add(1)
	}
	return wi, err
}

// List and Watch complete cache.ListerWatcher; the reflector calls the WithContext forms.
func (c *countingListerWatcher) List(opts metav1.ListOptions) (runtime.Object, error) {
	return c.ListWithContext(context.Background(), opts)
}

func (c *countingListerWatcher) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	return c.WatchWithContext(context.Background(), opts)
}

// IsWatchListSemanticsUnSupported forwards the wrapped client's opt-out, so a fake clientset keeps
// plain list and watch.
func (c *countingListerWatcher) IsWatchListSemanticsUnSupported() bool {
	return watchlist.DoesClientNotSupportWatchListSemantics(c.inner)
}
