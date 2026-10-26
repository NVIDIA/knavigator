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

# Installs jobset (https://github.com/kubernetes-sigs/jobset)
set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../..")
source "$REPO_HOME/scripts/env.sh"

JOBSET_VERSION=v0.8.1
printGreen Deploying jobset $JOBSET_VERSION

kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/${JOBSET_VERSION}/manifests.yaml

kubectl -n jobset-system patch deploy jobset-controller-manager \
  --patch-file=$REPO_HOME/charts/overrides/kwok-affinity-deployment-patch.yaml

wait_for_pods "jobset-system" 1

kubectl -n jobset-system wait --for=condition=ready pod -l control-plane=controller-manager --timeout=600s
