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
	"io"
	"os"

	"github.com/NVIDIA/knavigator/pkg/config"
)

type PauseTask struct {
	BaseTask
}

func newPauseTask(cfg *config.Task) *PauseTask {
	return &PauseTask{
		BaseTask: BaseTask{
			taskType: TaskPause,
			taskID:   cfg.ID,
		},
	}
}

// pauseInput is stdin; tests replace it.
var pauseInput io.Reader = os.Stdin

// Exec implements Runnable interface
func (task *PauseTask) Exec(ctx context.Context) error {
	fmt.Printf("-> Press 'Return' key to continue.")
	// The read cannot be interrupted, so it runs aside and a canceled context wins.
	res := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(pauseInput)
		scanner.Scan()
		res <- scanner.Err()
	}()
	select {
	case err := <-res:
		fmt.Println()
		return err
	case <-ctx.Done():
		fmt.Println()
		return ctx.Err()
	}
}
