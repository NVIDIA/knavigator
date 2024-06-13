#/bin/bash

set -e

KWOK_REPO=kubernetes-sigs/kwok
KWOK_RELEASE="v0.5.2"
REPO_HOME=${REPO_HOME:-.}

# Deploy KWOK controller
kubectl apply -f https://github.com/${KWOK_REPO}/releases/download/${KWOK_RELEASE}/kwok.yaml

# Deploy and adjust the stages
kubectl apply -f https://github.com/${KWOK_REPO}/releases/download/${KWOK_RELEASE}/stage-fast.yaml
kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-init-container-running-failed.yaml
kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-container-running-failed.yaml
kubectl apply -f ${REPO_HOME}/charts/overrides/kwok/pod-complete.yml
