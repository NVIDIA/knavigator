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

# Installs slurm-bridge (https://github.com/SlinkyProject/slurm-bridge)
set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../..")
source "$REPO_HOME/scripts/env.sh"

# helm v3.17 accepts a chart@sha256 reference but ignores the digest, so the chart pins below
# would pin nothing. v3.18.0 is the first release that checks the digest.
HELM_VERSION=$(helm version --template '{{.Version}}')
if [[ ! "$HELM_VERSION" =~ ^v([0-9]+)\.([0-9]+)\. ]] ||
    (( 10#${BASH_REMATCH[1]} < 3 || (10#${BASH_REMATCH[1]} == 3 && 10#${BASH_REMATCH[2]} < 18) )); then
  printRed "slurm-bridge.sh needs helm v3.18.0 or later, which checks chart digests; found helm $HELM_VERSION"
  exit 2
fi

SLURM_BRIDGE_VERSION=${SLURM_BRIDGE_VERSION:-v1.2.3}
SCHEDULER_PLUGINS_VERSION=v0.33.5
CHARTS=oci://ghcr.io/slinkyproject/charts
VALUES=$REPO_HOME/charts/overrides/slurm-bridge
CHART_VERSION=${SLURM_BRIDGE_VERSION#v}

# The chart and image digests of v1.2.3, so that version installs the same bits every time.
# Another SLURM_BRIDGE_VERSION installs its charts and their default images by tag.
CRDS_REF=$CHARTS/slurm-operator-crds:$CHART_VERSION
OPERATOR_REF=$CHARTS/slurm-operator:$CHART_VERSION
SLURM_REF=$CHARTS/slurm:$CHART_VERSION
BRIDGE_REF=$CHARTS/slurm-bridge:$CHART_VERSION
OPERATOR_IMAGES=()
SLURM_IMAGES=()
BRIDGE_IMAGES=()
if [[ "$SLURM_BRIDGE_VERSION" == "v1.2.3" ]]; then
  CRDS_REF=$CRDS_REF@sha256:fd30eb2d1e80b88dc6f0fe01becb3e96ff35a9eccb07a97072f3a105df963ce6
  OPERATOR_REF=$OPERATOR_REF@sha256:be8f99c89f9667b9dc2a64633007379a59f031938621bc116527bfddc995d956
  SLURM_REF=$SLURM_REF@sha256:f10b978a0adab2d24a3e0b3219eb847833fdcc388ff03c30b72739caaca8c67c
  BRIDGE_REF=$BRIDGE_REF@sha256:22411d138ba7e094ec54e241054e7fe8ad48db714cb0f7344f4225f0d3957f65
  OPERATOR_IMAGES=(--set operator.image.digest=sha256:b35e76c09e587f1bc902bcff228b6c14e63ffd173afebf33e627480e67bc36ee)
  # Slurm 26.05.4
  SLURM_IMAGES=(
    --set controller.slurmctld.image.digest=sha256:e5fa6666bd890f522a23cd292d17e57438f08260aaffd5aed652c0c6cf29687c
    --set controller.reconfigure.image.digest=sha256:e5fa6666bd890f522a23cd292d17e57438f08260aaffd5aed652c0c6cf29687c
    --set controller.logfile.image.digest=sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
    --set restapi.slurmrestd.image.digest=sha256:5009b8020b6dd734819f70b4cbe117934054dae4346dde6ecb50ae0ca2896c60
  )
  BRIDGE_IMAGES=(
    --set scheduler.image.tag=1.2.3@sha256:53db9c40f2b9fcdedd2aba800270f3496557bc1a1b995ea3ced2edfe110d0a32
    --set controllers.image.tag=1.2.3@sha256:e2e3e2c82d2c160821f3e4f6c5fe44c6962833add29909921eec624a51f0bd5c
  )
fi

printGreen Deploying slurm-bridge "$SLURM_BRIDGE_VERSION"

helm upgrade --install slurm-operator-crds "$CRDS_REF" --wait

# slurm-bridge watches PodGroups; gangs join one by label. Only the CRD, no coscheduling controller.
kubectl apply --server-side -f "https://raw.githubusercontent.com/kubernetes-sigs/scheduler-plugins/$SCHEDULER_PLUGINS_VERSION/config/crd/bases/scheduling.x-k8s.io_podgroups.yaml"

helm upgrade --install slurm-operator "$OPERATOR_REF" -n slinky --create-namespace \
  -f "$VALUES/slurm-operator.yaml" "${OPERATOR_IMAGES[@]}" --wait

# The Slurm auth key and the JWT key: 768 random bytes each, in base64. They are generated once
# and go straight into their Secret, so a reinstall keeps the keys Slurm already uses.
kubectl create namespace slurm --dry-run=client -o yaml | kubectl apply -f -
for secret in slurm-auth-slurm:slurm.key slurm-auth-jwt:jwt.key; do
  name=${secret%%:*}
  key=${secret#*:}
  if ! kubectl -n slurm get secret "$name" > /dev/null 2>&1; then
    kubectl -n slurm create secret generic "$name" \
      --from-file="$key"=<(head -c 768 /dev/urandom | base64 | tr -d '\n')
  fi
done

helm upgrade --install slurm "$SLURM_REF" -n slurm \
  -f "$VALUES/slurm.yaml" "${SLURM_IMAGES[@]}" --wait

# The operator creates slurmctld and slurmrestd from the chart's custom resources, so helm does not
# wait for them.
for name in slurmctld slurmrestd; do
  kubectl -n slurm wait --for=create pod -l app.kubernetes.io/name="$name" --timeout=300s
  kubectl -n slurm wait --for=condition=ready pod -l app.kubernetes.io/name="$name" --timeout=600s
done

# The operator mints slurm-bridge's slurmrestd token into Secret slurm-bridge-token, which the
# slurm-bridge pods mount.
kubectl apply -f "https://raw.githubusercontent.com/SlinkyProject/slurm-bridge/$SLURM_BRIDGE_VERSION/hack/token.yaml"
kubectl -n slurm wait --for=create secret/slurm-bridge-token --timeout=300s

helm upgrade --install slurm-bridge "$BRIDGE_REF" -n slurm \
  -f "$VALUES/slurm-bridge.yaml" "${BRIDGE_IMAGES[@]}" --wait

kubectl apply -f "$VALUES/pod-fail-on-delete.yaml"
