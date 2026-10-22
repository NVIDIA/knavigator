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

# Installs kueue (https://github.com/kubernetes-sigs/kueue)
set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../..")
source "$REPO_HOME/scripts/env.sh"

deploy_mpi_operator
deploy_kuberay_operator

KUEUE_VERSION=v0.11.4
printGreen Deploying kueue $KUEUE_VERSION

kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml

kubectl -n kueue-system patch deployment kueue-controller-manager \
  --patch-file=$REPO_HOME/charts/overrides/kwok-affinity-deployment-patch.yaml

wait_for_pods "kueue-system" 1

kubectl -n kueue-system wait --for=condition=ready pod -l control-plane=controller-manager --timeout=600s
