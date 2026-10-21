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

# Installs kai (https://github.com/kai-scheduler/KAI-Scheduler)
set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../..")
source "$REPO_HOME/scripts/env.sh"

deploy_mpi_operator

KAI_VERSION=${KAI_VERSION:-v0.18.6}
printGreen Deploying kai scheduler $KAI_VERSION

# The KAI operator crashloops without this CRD, which a GPU cluster gets from the GPU Operator.
kubectl apply -f - <<EOF
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: clusterpolicies.nvidia.com
spec:
  group: nvidia.com
  scope: Cluster
  names: {kind: ClusterPolicy, listKind: ClusterPolicyList, plural: clusterpolicies, singular: clusterpolicy}
  versions:
  - name: v1
    served: true
    storage: true
    subresources: {status: {}}
    schema:
      openAPIV3Schema:
        type: object
        x-kubernetes-preserve-unknown-fields: true
EOF

# Unlimited quota in the default queues, so quota does not hide scheduling decisions.
helm upgrade --install kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler -n kai-scheduler \
  --version="$KAI_VERSION" --create-namespace --wait --timeout 10m \
  --set defaultQueue.parentResources.cpu.quota=-1 \
  --set defaultQueue.parentResources.gpu.quota=-1 \
  --set defaultQueue.parentResources.memory.quota=-1 \
  --set defaultQueue.childResources.cpu.quota=-1 \
  --set defaultQueue.childResources.gpu.quota=-1 \
  --set defaultQueue.childResources.memory.quota=-1

# The scheduler Deployment can lose a race with its ServiceAccount on a fresh install.
kubectl -n kai-scheduler wait --for=create sa/scheduler --timeout=180s
kubectl -n kai-scheduler rollout restart deploy/kai-scheduler-default
kubectl -n kai-scheduler rollout status deploy/kai-scheduler-default --timeout=180s
