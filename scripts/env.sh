# Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

function printRed() {
  echo -e "${RED}$@${NC}"
}

function printGreen() {
  echo -e "${GREEN}$@${NC}"
}

function printYellow() {
  echo -e "${YELLOW}$@${NC}"
}

function printBlue() {
  echo -e "${BLUE}$@${NC}"
}

### check for command
function fail_if_command_not_found() {
  local command_name="$1"
  if ! command -v $command_name &> /dev/null; then
    printRed "$command_name is not installed"
    exit 1
  fi
}

### wait for specific number of pods in a namespace
function wait_for_pods() {
  local namespace=$1
  local pods=$2
  local wait_time=60
  local sleep_interval=5
  local elapsed_time=0

  while true; do
    count=$(kubectl get pods -n $namespace --no-headers 2>/dev/null | wc -l)
    if [ "$count" -eq $pods ]; then
      break
    fi
    echo "current pods $count, expecting $pods"

    sleep "$sleep_interval"
    elapsed_time=$((elapsed_time + sleep_interval))
    if [ "$elapsed_time" -gt "$wait_time" ]; then
      exit 1
    fi
  done
}

# KWOK
#
function deploy_kwok() {
  KWOK_REPO=kubernetes-sigs/kwok
  KWOK_RELEASE="v0.8.0"
  printGreen Deploying KWOK $KWOK_RELEASE

  # Deploy KWOK controller
  kubectl apply -f https://github.com/${KWOK_REPO}/releases/download/${KWOK_RELEASE}/kwok.yaml

  # Deploy and adjust the stages
  kubectl apply -f https://github.com/${KWOK_REPO}/releases/download/${KWOK_RELEASE}/stage-fast.yaml
  kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-init-container-running-failed.yaml
  kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/chaos/pod-container-running-failed.yaml
  #kubectl apply -f https://github.com/${KWOK_REPO}/raw/main/kustomize/stage/pod/general/pod-complete.yaml

  # kind's DaemonSets tolerate every taint. On virtual nodes, the pod-complete stage would finish
  # their pods and the DaemonSet controller would recreate them for the whole run.
  for ds in kindnet kube-proxy; do
    if kubectl -n kube-system get ds $ds &> /dev/null; then
      kubectl -n kube-system patch ds $ds --type merge -p '{"spec":{"template":{"spec":{"affinity":{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"type","operator":"NotIn","values":["kwok"]}]}]}}}}}}}'
    fi
  done
}

# Prometheus
#
function deploy_prometheus() {
  PROMETHEUS_STACK_VERSION=61.5.0
  printGreen Deploying Prometheus stack $PROMETHEUS_STACK_VERSION

  helm repo add --force-update prometheus-community https://prometheus-community.github.io/helm-charts

  helm upgrade --install -n monitoring --create-namespace kube-prometheus-stack \
    prometheus-community/kube-prometheus-stack \
    --version=$PROMETHEUS_STACK_VERSION --wait \
    --set alertmanager.enabled=false \
    --set grafana.enabled=false \
    --set nodeExporter.enabled=false \
    --set defaultRules.rules.alertmanager=false \
    --set defaultRules.rules.nodeExporterAlerting=false \
    --set defaultRules.rules.nodeExporterRecording=false \
    --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
    --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false

  kubectl -n monitoring wait --for=condition=ready pod -l app.kubernetes.io/instance=kube-prometheus-stack --timeout=600s

  printGreen Deploying Node Resource Exporter

  helm upgrade --install -n monitoring node-resource-exporter --wait $REPO_HOME/charts/node-resource-exporter

  kubectl -n monitoring wait --for=condition=ready pod -l app.kubernetes.io/name=node-resource-exporter --timeout=600s

  PUSHGATEWAY_VERSION=3.9.1
  printGreen Deploying Pushgateway $PUSHGATEWAY_VERSION

  helm upgrade --install -n monitoring pushgateway prometheus-community/prometheus-pushgateway \
    --version=$PUSHGATEWAY_VERSION --wait \
    --set serviceMonitor.enabled=true
}

# Operators that workload managers in scripts/schedulers share
#
function deploy_mpi_operator() {
  MPI_OPERATOR_VERSION=v0.6.0
  printGreen Deploying mpi-operator $MPI_OPERATOR_VERSION
  kubectl apply --server-side --force-conflicts -f https://raw.githubusercontent.com/kubeflow/mpi-operator/$MPI_OPERATOR_VERSION/deploy/v2beta1/mpi-operator.yaml
}

function deploy_kuberay_operator() {
  RAY_VERSION=v1.3.2
  printGreen Deploying kuberay-operator $RAY_VERSION

  helm repo add kuberay https://ray-project.github.io/kuberay-helm/
  helm repo update
  helm install kuberay-operator kuberay/kuberay-operator -n kuberay-system --create-namespace \
    --version=$RAY_VERSION --wait
}
