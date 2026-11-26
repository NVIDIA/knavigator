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

set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/../../..")

usage() {
  echo "usage: $0 placement <results-dir> [knavigator flags...]" >&2
  exit 2
}

[ $# -ge 2 ] && [ -n "$2" ] || usage
# The occupier, four checked jobs one after another, then the unconstrained and oversized jobs.
case "$1" in
  placement) WORKFLOWS="config-nodes.yaml,config-kai.yaml,run-occupier.yaml,run-checked.yaml,run-report.yaml" ;;
  *) usage ;;
esac
RESULTS=$2
shift 2
# The workflows name their templates relative to the repository root, so the run starts there.
# A relative results dir is taken from the caller's directory first.
case "$RESULTS" in
  /*) ;;
  *) RESULTS="$PWD/${RESULTS#./}" ;;
esac
cd "$REPO_HOME"

# NODE_ADDRESS, when set, becomes the virtual nodes' InternalIP. Extra flags, such as -var, pass through.
"$REPO_HOME/bin/knavigator" -results "$RESULTS" -results.timeout 20m -results.stall 8m \
  -var "node_address=${NODE_ADDRESS:-}" "$@" \
  -workflow "$REPO_HOME/resources/benchmarks/topology-placement/workflows/{$WORKFLOWS}"
