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
	"fmt"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type SleepTask struct {
	BaseTask

	sleepTaskParams
}

type sleepTaskParams struct {
	Timeout time.Duration `yaml:"timeout"`
}

func newSleepTask(cfg *config.Task) (*SleepTask, error) {
	task := &SleepTask{
		BaseTask: BaseTask{
			taskType: TaskSleep,
			taskID:   cfg.ID,
		},
	}

	if err := task.validate(cfg.Params); err != nil {
		return nil, err
	}

	return task, nil
}

// validate initializes and validates parameters for SleepTask
func (task *SleepTask) validate(params map[string]interface{}) error {
	data, err := yaml.Marshal(params)
	if err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}
	if err = yaml.Unmarshal(data, &task.sleepTaskParams); err != nil {
		return fmt.Errorf("%s: failed to parse parameters: %v", task.ID(), err)
	}

	if task.Timeout == 0 {
		return fmt.Errorf("%s: 'timeout' parameter have value", task.ID())
	}

	return nil
}

// Exec implements Runnable interface
func (task *SleepTask) Exec(ctx context.Context) error {
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, task.Timeout)
	defer cancel()
	<-ctx.Done()
	return nil
}
