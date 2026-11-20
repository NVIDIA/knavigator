#! /bin/bash

set -xe

REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)
source $REPO_HOME/scripts/env.sh

SCHEDULER=${1:?usage: $0 <scheduler>}

# Install KWOK node simulator
deploy_kwok

# The default scheduler (k8s) needs no install
if [ -f "$REPO_HOME/scripts/schedulers/$SCHEDULER.sh" ]; then
  "$REPO_HOME/scripts/schedulers/$SCHEDULER.sh"
fi

"$REPO_HOME/scripts/conformance.sh" "$SCHEDULER"
