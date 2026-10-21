#! /bin/bash

set -xe

REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)
source $REPO_HOME/scripts/env.sh

# Install KWOK node simulator
deploy_kwok
# Complete pods after their ttl, so the workflow sees them running
kubectl apply -f ${REPO_HOME}/charts/overrides/kwok/pod-complete.yaml

# Install Kueue
${REPO_HOME}/scripts/schedulers/kueue.sh

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/kueue/test-job.yaml -v 4 -cleanup
