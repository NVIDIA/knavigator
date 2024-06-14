#! /bin/bash

set -x -e

export REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)

# Install KWOK node simulator
${REPO_HOME}/scripts/install_kwok.sh

# Install JobSet
JOBSET_VERSION=v0.5.2

kubectl apply --server-side -f https://github.com/kubernetes-sigs/jobset/releases/download/${JOBSET_VERSION}/manifests.yaml

kubectl -n jobset-system wait --for=condition=ready pod -l control-plane=controller-manager --timeout=60s

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/k8s/test-jobset.yml -cleanup
