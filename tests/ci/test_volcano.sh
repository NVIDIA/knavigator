#! /bin/bash

set -xe

REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)
source $REPO_HOME/scripts/env.sh

# Install KWOK node simulator
deploy_kwok

# Install Volcano
${REPO_HOME}/scripts/schedulers/volcano.sh

# The example workflow runs at this priority, as docs/examples/volcano/volcano.md describes.
kubectl create priorityclass normal-priority --value=100000

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/volcano/test-job.yml -v 4 -cleanup
