#! /bin/bash

set -x -e

export REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)

# Install KWOK node simulator
${REPO_HOME}/scripts/install_kwok.sh

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/k8s/test-job.yml
