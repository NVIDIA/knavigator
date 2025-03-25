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
cat << EOF
  1: jobset (https://github.com/kubernetes-sigs/jobset)
  2: kueue (https://github.com/kubernetes-sigs/kueue)
  3: volcano (https://github.com/volcano-sh/volcano)
  4: yunikorn (https://github.com/apache/yunikorn-core)
  5: run:ai (https://www.run.ai)
  6: kai (https://github.com/NVIDIA/KAI-Scheduler)
  7: combined: coscheduler plugin + jobset + kueue
EOF
read -p "> " choice

case "$choice" in
  1)
    deploy_jobset
    ;;
  2)
    deploy_kueue
    ;;
  3)
    deploy_volcano
    ;;
  4)
    deploy_yunikorn
    ;;
  5)
    deploy_runai
    ;;
  6)
    deploy_kai
    ;;
  7)
    deploy_scheduler_plugins
    deploy_jobset
    deploy_kueue
    ;;
esac

printYellow Cluster is ready
