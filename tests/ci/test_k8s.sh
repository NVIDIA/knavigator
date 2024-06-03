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

# Run knavigator with an example test
${REPO_HOME}/bin/knavigator -workflow ${REPO_HOME}/resources/workflows/k8s/test-job.yml
