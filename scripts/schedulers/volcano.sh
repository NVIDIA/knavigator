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

# Installs volcano (https://github.com/volcano-sh/volcano)
set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../..")
source "$REPO_HOME/scripts/env.sh"

VOLCANO_VERSION=v1.11.2
printGreen Deploying volcano $VOLCANO_VERSION

helm repo add --force-update volcano-sh https://volcano-sh.github.io/helm-charts

helm upgrade --install volcano volcano-sh/volcano -n volcano-system --create-namespace \
  --version=$VOLCANO_VERSION --wait \
  --set-json 'affinity={"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"type","operator":"NotIn","values":["kwok"]}]}]}}}'

for app in volcano-admission volcano-controller volcano-scheduler; do
  kubectl -n volcano-system wait --for=condition=ready pod -l app=$app --timeout=600s
done

# Wait until volcano webhook is ready
# TODO: we need a deterministric way to check if it's ready
sleep 10
