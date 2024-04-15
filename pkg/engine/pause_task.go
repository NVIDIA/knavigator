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
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type PauseTask struct {
	BaseTask
}

func newPauseTask(log logr.Logger, cfg *config.Task) *PauseTask {
	return &PauseTask{
		BaseTask: BaseTask{
			log:      log,
			taskType: TaskPause,
			taskID:   cfg.ID,
		},
	}
}

// Exec implements Runnable interface
func (task *PauseTask) Exec(ctx context.Context) error {
	fmt.Printf("-> Press 'Return' key to continue.")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		break
	}
	fmt.Println()
	return scanner.Err()
}
