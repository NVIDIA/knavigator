#!/bin/bash

# Copyright (c) 2024-2025, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../../)

$REPO_HOME/bin/knavigator -workflow "$REPO_HOME/resources/benchmarks/scaling/workflows/{config-nodes.yaml,config-yunikorn.yaml,run-test-single.yaml}"
