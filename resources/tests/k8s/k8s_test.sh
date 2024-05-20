#! /bin/sh

REPO_HOME=$(readlink -f $(dirname $(readlink -f "$0"))/../../../)

# Install KWOK node simulator
KWOK_REPO=kubernetes-sigs/kwok
KWOK_LATEST_RELEASE=v0.5.2

kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/kwok.yaml"
kubectl apply -f "https://github.com/${KWOK_REPO}/releases/download/${KWOK_LATEST_RELEASE}/stage-fast.yaml"
kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-init-container-running-failed.yaml
kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-container-running-failed.yaml
kubectl apply -f ${REPO_HOME}/charts/overrides/kwok/pod-complete.yml

# Add virtual nodes to the cluster
helm install virtual-nodes ${REPO_HOME}/charts/virtual-nodes -f ${REPO_HOME}/charts/virtual-nodes/values-example.yaml
kubectl get nodes

# Run knavigator with an example test
kubectl create ns k8s-test
kubectl create priorityclass normal-priority --value=100000
./bin/knavigator -tasks ${REPO_HOME}/resources/tests/k8s/test-job.yml
kubectl get job -n k8s-test
