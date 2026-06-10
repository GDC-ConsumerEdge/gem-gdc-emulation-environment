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

set -euo pipefail
# shellcheck source=/dev/null
source /workspace/state/env

export CLUSTER_NAME="${CLUSTER_NAME}"
export PROJECT_ID="${PROJECT_ID}"
export TF_STATE_BUCKET="${TF_STATE_BUCKET}"

# Stage SSH key from persisted /workspace directory
install -m 700 -d /root/.ssh
install -m 600 /workspace/.ssh/google_compute_engine /root/.ssh/google_compute_engine
[[ -f /workspace/.ssh/config ]] && install -m 600 /workspace/.ssh/config /root/.ssh/config

if [[ -f /workspace/state/failed-stage ]]; then
  echo "Skipping Chainsaw E2E, earlier step failed ($(cat /workspace/state/failed-stage))."
  exit 0
fi

echo "Waiting for Admin Workstation to be ready..."
gcloud compute ssh gem@gem-admin-ws --zone=us-central1-a --command="echo Workstation is reachable."

echo "Creating temporary directory on admin workstation..."
REMOTE_TEMP_DIR=$(gcloud compute ssh gem@gem-admin-ws --zone=us-central1-a --command="mktemp -d -t chainsaw-tests-XXXXXX")

# Ensure remote temporary files are cleaned up on script exit
cleanup() {
  echo "Stopping / cleaning up temporary test files on admin workstation..."
  gcloud compute ssh gem@gem-admin-ws --zone=us-central1-a --command="rm -rf ${REMOTE_TEMP_DIR}" || true
}
trap cleanup EXIT


echo "Copying E2E tests to workstation..."
gcloud compute scp --recurse tests/ gem@gem-admin-ws:"${REMOTE_TEMP_DIR}"/ --zone=us-central1-a


echo "Running GEM conformance tests on admin workstation..."
if ! gcloud compute ssh gem@gem-admin-ws --zone=us-central1-a --command="
  export KUBECONFIG=/home/gem/bmctl-workspace/${CLUSTER_NAME}/${CLUSTER_NAME}-kubeconfig
  cd ${REMOTE_TEMP_DIR}
  chainsaw test tests/e2e --config tests/e2e/chainsaw-configuration.yaml
"; then
  echo "chainsaw-e2e" >/workspace/state/failed-stage
  exit 1
fi

echo "✅ Chainsaw E2E tests completed successfully."
