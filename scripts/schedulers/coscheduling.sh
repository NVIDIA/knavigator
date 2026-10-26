#!/bin/bash
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

# Installs the coscheduling plugin with jobset and kueue (https://github.com/kubernetes-sigs/scheduler-plugins)
set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../..")
source "$REPO_HOME/scripts/env.sh"

printGreen Deploying scheduler-plugins
SCHEDULER_PLUGINS_VERSION=v0.29.7

helm upgrade --install --repo https://scheduler-plugins.sigs.k8s.io scheduler-plugins scheduler-plugins \
  -n scheduler-plugins --create-namespace --version $SCHEDULER_PLUGINS_VERSION \
  --set-json 'scheduler.affinity={"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"type","operator":"NotIn","values":["kwok"]}]}]}}}' \
  --set-json 'controller.affinity={"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"type","operator":"NotIn","values":["kwok"]}]}]}}}'

"$REPO_HOME/scripts/schedulers/jobset.sh"
"$REPO_HOME/scripts/schedulers/kueue.sh"
