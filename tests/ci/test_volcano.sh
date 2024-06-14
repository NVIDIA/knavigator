#! /bin/bash

set -x -e

export REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)

# Install KWOK node simulator
${REPO_HOME}/scripts/install_kwok.sh

# Install Volcano
helm repo add volcano-sh https://volcano-sh.github.io/helm-charts
helm repo update
helm install volcano volcano-sh/volcano -n volcano-system --create-namespace --wait

for app in volcano-admission volcano-controller volcano-scheduler; do
  kubectl -n volcano-system wait --for=condition=ready pod -l app=$app --timeout=60s
done

# Wait until volcano webhook is ready
# TODO: we need a deterministric way to check if it's ready
sleep 10

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/volcano/test-job.yml
