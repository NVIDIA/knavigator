#! /bin/bash -x

REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../)

# Install KWOK node simulator
KWOK_REPO=kubernetes-sigs/kwok
KWOK_LATEST_RELEASE=v0.5.2

kubectl apply -f https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/kwok.yaml
kubectl apply -f https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/stage-fast.yaml
kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-init-container-running-failed.yaml
kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-container-running-failed.yaml
kubectl apply -f ${REPO_HOME}/charts/overrides/kwok/pod-complete.yml

# Install Kueue
KUEUE_VERSION=v0.6.2

kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/prometheus.yaml

# Wait until kueue webhook is ready
# TODO: we need a deterministric way to check if it's ready
sleep 10

# Deploy cluster and local queues
kubectl apply -f ${REPO_HOME}/docs/examples/kueue/queues.yml

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/kueue/test-job.yml
