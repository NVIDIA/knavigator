#! /bin/bash
# Copyright (c) 2026, NVIDIA CORPORATION & AFFILIATES. All rights reserved.
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

set -xe

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../..")
source "$REPO_HOME/scripts/env.sh"

# Install KWOK node simulator
deploy_kwok
# Complete pods after their ttl, so the workflow sees them running
kubectl apply -f "$REPO_HOME/charts/overrides/kwok/pod-complete.yaml"

# Install slurm-bridge
"$REPO_HOME/scripts/schedulers/slurm-bridge.sh"

# Run knavigator with an example test
"$REPO_HOME/bin/knavigator" -workflow "$REPO_HOME/resources/workflows/slurm-bridge/test-job.yaml" -v 4 -cleanup
