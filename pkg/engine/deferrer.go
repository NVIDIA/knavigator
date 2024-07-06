/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
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

package engine

import (
	"context"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/workqueue"
	log "k8s.io/klog/v2"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type executor interface {
	RunTask(context.Context, *config.Task) error
}

type Deferrer struct {
	executor executor
	queue    workqueue.DelayingInterface
	client   kubernetes.Interface
	wg       sync.WaitGroup
}

func NewDereffer(client kubernetes.Interface, executor executor) *Deferrer {
	return &Deferrer{
		executor: executor,
		queue:    workqueue.NewDelayingQueue(),
		client:   client,
	}
}

func (d *Deferrer) ScheduleTermination(taskID string) {
	d.wg.Add(1)
	d.queue.Add(taskID)
}

func (d *Deferrer) Start(ctx context.Context) {
	go d.start(ctx)
}

func (d *Deferrer) start(ctx context.Context) {
	for {
		// Get an item from the queue
		obj, shutdown := d.queue.Get()
		if shutdown {
			break
		}

		switch v := obj.(type) {
		case string:
			log.Info("Wait for running pods", "taskID", v)
			err := d.executor.RunTask(ctx, &config.Task{
				ID:   "status",
				Type: TaskCheckPod,
				Params: map[string]interface{}{
					"refTaskId": v,
					"status":    "Running",
					"timeout":   "24h",
				},
			})
			if err != nil {
				log.Error(err, "Failed to watch pods")
				d.wg.Done()
			} else {
				log.Info("AddTask", "type", TaskDeleteObj)
				d.queue.AddAfter(&config.Task{
					ID:     "delete",
					Type:   TaskDeleteObj,
					Params: map[string]interface{}{"refTaskId": v},
				}, 5*time.Second)
			}

		case *config.Task:
			log.Info("Deferrer initiates task", "type", v.Type, "ID", v.ID)

			err := d.executor.RunTask(ctx, v)
			if err != nil {
				log.Error(err, "failed to execute task", "type", v.Type, "ID", v.ID)
			}
			d.wg.Done()
		}

		// Mark the item as done
		d.queue.Done(obj)
	}
}

func (d *Deferrer) Wait(ctx context.Context, timeout time.Duration) error {
	log.Info("Waiting for deferrer to complete task")
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan struct{})

	go func() {
		d.wg.Wait()
		done <- struct{}{}
	}()

	select {
	case <-done:
		d.queue.ShutDown()
		log.Info("Deferrer stopped")
		return nil
	case <-ctx.Done():
		log.Info("Deferrer didn't stop in allocated time")
		return ctx.Err()
	}
}
