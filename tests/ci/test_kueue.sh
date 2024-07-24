#! /bin/bash

set -x -e

export REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)

# Install KWOK node simulator
${REPO_HOME}/scripts/install_kwok.sh

# Install Kueue
KUEUE_VERSION=v0.8.0

kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml

kubectl -n kueue-system wait --for=condition=ready pod -l control-plane=controller-manager --timeout=60s

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/kueue/test-job.yml -cleanup
