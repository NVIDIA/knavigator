#! /bin/bash

set -x -e

export REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)

# Install KWOK node simulator
${REPO_HOME}/scripts/install_kwok.sh

# Install YuniKorn
helm repo add --force-update yunikorn https://apache.github.io/yunikorn-release
helm install yunikorn yunikorn/yunikorn -n yunikorn --create-namespace --wait

kubectl -n yunikorn wait --for=condition=ready pod -l app=yunikorn --timeout=60s

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/yunikorn/test-job.yml -cleanup
