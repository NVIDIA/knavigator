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

cluster_name=kind
workers=0
config=""

function usage() {
  cat <<EOF
Usage: $(basename "$0") [--name NAME] [--workers N | --config PATH]

Creates a kind cluster and deploys Prometheus, KWOK, and a workload manager of your choice.

  --name NAME     kind cluster name (default: kind)
  --workers N     worker nodes besides the control plane (default: 0)
  --config PATH   kind config to use instead of --workers
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --name) cluster_name=${2:?--name needs a value}; shift 2 ;;
    --workers) workers=${2:?--workers needs a value}; shift 2 ;;
    --config) config=${2:?--config needs a value}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) printRed "Unknown option: $1"; usage; exit 1 ;;
  esac
done

if [[ ! $workers =~ ^[0-9]+$ ]]; then
  printRed "--workers must be 0 or more"
  exit 1
fi

printYellow Creating test cluster

echo "This script installs a kind cluster and deploys Prometheus, KWOK, and workload manager of your choice"

fail_if_command_not_found kind
fail_if_command_not_found helm
fail_if_command_not_found kubectl

# Kind nodes fail to start or register when these host limits are low.
if [[ $OSTYPE == linux* ]]; then
  for limit in kernel.keys.maxkeys=2000 fs.inotify.max_user_instances=65535 fs.inotify.max_user_watches=1048576; do
    if (( $(sysctl -n "${limit%=*}") < ${limit#*=} )); then
      printYellow "Kind nodes may not start; raise the limit with: sudo sysctl -w $limit"
    fi
  done
fi

function create_cluster() {
  if [[ -z "$config" ]]; then
    config=$(mktemp)
    trap 'rm -f "$config"' EXIT
    {
      echo "kind: Cluster"
      echo "apiVersion: kind.x-k8s.io/v1alpha4"
      echo "nodes:"
      echo "- role: control-plane"
      for ((i = 0; i < workers; i++)); do echo "- role: worker"; done
    } > "$config"
  fi
  kind create cluster --name "$cluster_name" --config "$config"
}

if kind get clusters 2>/dev/null | grep -Fxq "$cluster_name"; then
  echo "Kind cluster $cluster_name exists. Delete and recreate it? (y/n)"
  read -p "> " choice
  if [[ "$choice" == "y" ]]; then
    kind delete cluster --name "$cluster_name"
    create_cluster
  fi
else
  create_cluster
fi
kubectl config use-context "kind-$cluster_name"

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
