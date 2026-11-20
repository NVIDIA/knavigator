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

# Runs every conformance test that has a workflow for the scheduler, on the current cluster, and
# prints which ones pass. With CONFORMANCE_PUSHGATEWAY set, it also pushes the results for Grafana.
# Usage: scripts/conformance.sh <scheduler> [tests directory]
set -e

REPO_HOME=$(readlink -f "$(dirname "$(readlink -f "$0")")/..")
source "$REPO_HOME/scripts/env.sh"

SCHEDULER=${1:?usage: $0 <scheduler> [tests directory]}
TESTS=$(readlink -f "${2:-$REPO_HOME/resources/conformance}")
NAMESPACE=conformance

metrics=""
summary=""
for dir in "$TESTS"/*/; do
  test=$(basename "$dir")
  [ -f "$dir/$SCHEDULER.yaml" ] || continue
  pass=$(sed -n 's/^Pass: //p' "$dir/README.md" 2>/dev/null)

  printBlue "Running $test"
  if [ -x "$dir/setup.sh" ]; then
    "$dir/setup.sh"
  fi
  log=$(mktemp)
  # Workflows name their templates relative to their own directory.
  if (cd "$dir" && "$REPO_HOME/bin/knavigator" -workflow "$SCHEDULER.yaml" -cleanup -v 2) >"$log" 2>&1; then
    result=1
    printGreen "$test: supports"
    summary+="$test: supports"$'\n'
  else
    result=0
    reason=$(grep -v '^\s*$' "$log" | tail -1)
    printRed "$test: does not support ($reason)"
    summary+="$test: does not support"$'\n'
  fi
  rm -f "$log"
  # The namespace also holds pods that cleanup does not know about, such as gang placeholders.
  kubectl delete namespace "$NAMESPACE" --ignore-not-found --wait >/dev/null

  pass=${pass//\\/\\\\}
  metrics+="knavigator_conformance{test=\"$test\"} $result"$'\n'
  metrics+="knavigator_conformance_criteria{test=\"$test\",pass=\"${pass//\"/\\\"}\"} 1"$'\n'
done

if [ -z "$metrics" ]; then
  printRed "No test in $TESTS has a $SCHEDULER.yaml workflow"
  exit 1
fi

printf '\n%s results:\n%s' "$SCHEDULER" "$summary"

if [ -n "$CONFORMANCE_PUSHGATEWAY" ]; then
  # PUT replaces the scheduler's previous results, so a removed test does not linger.
  printf '%s' "$metrics" | curl -fsS -X PUT --data-binary @- \
    "$CONFORMANCE_PUSHGATEWAY/metrics/job/knavigator-conformance/scheduler/$SCHEDULER"
fi
