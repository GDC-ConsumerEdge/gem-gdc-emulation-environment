#!/bin/bash
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Runs only when a prior stage failed AND DESTROY_ON_FAILURE=true.  This is best-effort
# its own failure will not mask the original build failure

set -euo pipefail
# shellcheck source=/dev/null
source /workspace/state/env

export CLUSTER_NAME="${CLUSTER_NAME}"
export PROJECT_ID="${PROJECT_ID}"
export TF_STATE_BUCKET="${TF_STATE_BUCKET}"


if [[ ! -f /workspace/state/failed-stage ]]; then
  echo "No failure recorded; leaving cluster ${CLUSTER_NAME} in place."
  exit 0
fi

if [[ "${DESTROY_ON_FAILURE}" != "true" ]]; then
  echo "Failure detected ($(cat /workspace/state/failed-stage)) but _DESTROY_ON_FAILURE=false; preserving cluster for inspection."
  exit 0
fi

echo "⚠️  Failure detected ($(cat /workspace/state/failed-stage)); tearing down ${CLUSTER_NAME}."

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bash "${SCRIPT_DIR}/ansible-cleanup.sh" || true
bash "${SCRIPT_DIR}/tf-destroy.sh" || true
