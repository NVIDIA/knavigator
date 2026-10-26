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

# Installs yunikorn (https://github.com/apache/yunikorn-core)
set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../..")
source "$REPO_HOME/scripts/env.sh"

YUNIKORN_VERSION=v1.6.2
printGreen Deploying yunikorn $YUNIKORN_VERSION

helm repo add --force-update yunikorn https://apache.github.io/yunikorn-release

helm upgrade --install yunikorn yunikorn/yunikorn -n yunikorn --create-namespace \
  --version=$YUNIKORN_VERSION --wait \
  --set-json 'affinity={"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"type","operator":"NotIn","values":["kwok"]}]}]}}}'

kubectl -n yunikorn wait --for=condition=ready pod -l app=yunikorn --timeout=600s
