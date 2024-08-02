#! /bin/bash

set -xe

REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)
source $REPO_HOME/scripts/env.sh

# Install KWOK node simulator
deploy_kwok

# Install YuniKorn
deploy_yunikorn

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/yunikorn/test-job.yml -v 4 -cleanup
