#/bin/bash

set -e
REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../)

set -x
source $REPO_HOME/scripts/env.sh

deploy_kwok
