#!/bin/bash

# Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
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

REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../)

source $REPO_HOME/scripts/env.sh

printYellow Creating test cluster

echo "This script installs a kind cluster and deploys Prometheus, KWOK, and workload manager of your choice"

fail_if_command_not_found kind
fail_if_command_not_found helm
fail_if_command_not_found kubectl

if kind get clusters > /dev/null 2>&1; then
  echo "Kind is running. Delete? (y/n)"
  read -p "> " choice
  if [[ "$choice" == "y" ]]; then
    kind delete cluster
    kind create cluster
    # --image=kindest/node:v1.29.7
  fi
else
  kind create cluster
  # --image=kindest/node:v1.29.7
fi

deploy_prometheus

deploy_kwok
kubectl apply -f $REPO_HOME/charts/overrides/kwok/pod-complete.yaml

echo ""
printYellow "Select workload manager or leave it blank to skip:"
schedulers=("$REPO_HOME"/scripts/schedulers/*.sh)
for i in "${!schedulers[@]}"; do
  echo "  $((i + 1)): $(sed -n 's/^# Installs //p' "${schedulers[$i]}")"
done
read -p "> " choice

if [[ -n "$choice" ]]; then
  # Reject 0, negatives, and words before indexing; bash would treat them as arithmetic.
  if [[ ! $choice =~ ^[1-9][0-9]*$ ]] || ((choice > ${#schedulers[@]})); then
    printRed "No workload manager $choice"
    exit 1
  fi
  "${schedulers[$((choice - 1))]}"
fi

printYellow Cluster is ready
