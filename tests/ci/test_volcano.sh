#! /bin/bash

set -xe

REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)
source $REPO_HOME/scripts/env.sh

# Install KWOK node simulator
deploy_kwok

# Install Volcano
deploy_volcano

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/volcano/test-job.yml
