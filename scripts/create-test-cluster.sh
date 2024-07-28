#!/bin/bash

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
  fi
else
  kind create cluster
fi

deploy_prometheus

deploy_kwok

echo ""
printYellow "Select workload manager or leave it blank to skip:"
cat << EOF
  1: jobset (https://github.com/kubernetes-sigs/jobset)
  2: kueue (https://github.com/kubernetes-sigs/kueue)
  3: volcano (https://github.com/volcano-sh/volcano)
  4: yunikorn (https://github.com/apache/yunikorn-core)
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
esac

printYellow Cluster is ready
