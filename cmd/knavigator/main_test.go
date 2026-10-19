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

package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/knavigator/pkg/config"
)

func TestSpeedUp(t *testing.T) {
	w := &config.Workflow{Tasks: []*config.Task{
		{Type: "Sleep", Params: map[string]interface{}{"timeout": "2m"}},
		{Type: "SubmitObj", Params: map[string]interface{}{"params": map[string]interface{}{"ttl": "5m", "workers": 4}}},
		{Type: "CheckPod", Params: map[string]interface{}{"timeout": "10m"}},
	}}

	speedUp([]*config.Workflow{w}, 60)

	require.Equal(t, "2s", w.Tasks[0].Params["timeout"])
	require.Equal(t, "5s", w.Tasks[1].Params["params"].(map[string]interface{})["ttl"])
	require.Equal(t, 4, w.Tasks[1].Params["params"].(map[string]interface{})["workers"])
	require.Equal(t, "10m", w.Tasks[2].Params["timeout"])
}
