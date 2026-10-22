#!/bin/bash
# Copyright (c) 2026, NVIDIA CORPORATION & AFFILIATES. All rights reserved.
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

# Installs run:ai (https://www.run.ai)
set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../..")
source "$REPO_HOME/scripts/env.sh"

printGreen Deploying run:ai
TRAINING_OPERATOR_VERSION=v1.8.0
MPI_OPERATOR_VERSION=v0.4.0
RUNAI_VERSION=2.18.49

if [[ -z "$RUNAI_CONTROL_PLANE_URL" ]] || [[ -z "$RUNAI_CLIENT_SECRET" ]] || [[ -z "$RUNAI_CLUSTER_ID" ]]; then
  printRed "
Run:ai deployment requires environment variables:
  RUNAI_CONTROL_PLANE_URL : control plane URL
  RUNAI_CLIENT_SECRET     : client secret
  RUNAI_CLUSTER_ID        : cluster UID"
  exit 1
fi

kubectl apply -k "github.com/kubeflow/training-operator/manifests/overlays/standalone?ref=$TRAINING_OPERATOR_VERSION"

kubectl patch deployment training-operator -n kubeflow --type='json' \
  -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args", "value": ["--enable-scheme=tfjob", "--enable-scheme=pytorchjob", "--enable-scheme=xgboostjob"]}]'

kubectl delete crd mpijobs.kubeflow.org

kubectl apply -f https://raw.githubusercontent.com/kubeflow/mpi-operator/$MPI_OPERATOR_VERSION/deploy/v2beta1/mpi-operator.yaml

openssl req -x509 -newkey rsa:4096 -sha256 -days 365 -nodes -out certificate.pem -keyout private_key.pem -subj "/CN=$RUNAI_CONTROL_PLANE_URL"

kubectl create ns runai

kubectl create secret tls runai-cluster-domain-tls-secret -n runai --cert certificate.pem --key private_key.pem

helm repo add --force-update runai https://runai.jfrog.io/artifactory/api/helm/run-ai-charts

helm upgrade --install runai-cluster runai/runai-cluster -n runai \
  --version="$RUNAI_VERSION" --create-namespace --wait \
  --set controlPlane.url=$RUNAI_CONTROL_PLANE_URL \
  --set controlPlane.clientSecret=$RUNAI_CLIENT_SECRET \
  --set cluster.uid=$RUNAI_CLUSTER_ID \
  --set cluster.url=https://example.com \
  --set-json 'affinity={"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"type","operator":"NotIn","values":["kwok"]}]}]}}}'
