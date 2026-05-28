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

if [[ ! -f /workspace/state/failed-stage ]]; then
  echo "No failure recorded; leaving cluster ${CLUSTER_NAME} in place."
  exit 0
fi

if [[ "${DESTROY_ON_FAILURE}" != "true" ]]; then
  echo "Failure detected ($(cat /workspace/state/failed-stage)) but _DESTROY_ON_FAILURE=false; preserving cluster for inspection."
  exit 0
fi

echo "⚠️  Failure detected ($(cat /workspace/state/failed-stage)); tearing down ${CLUSTER_NAME}."

install -m 700 -d /root/.ssh
install -m 600 /workspace/.ssh/google_compute_engine /root/.ssh/google_compute_engine
[[ -f /workspace/.ssh/config ]] && install -m 600 /workspace/.ssh/config /root/.ssh/config

cd ansible
# Best-effort: cleanup needs the workstation reachable and the cluster far
# enough along to have a kubeconfig.
ansible-playbook cleanup.yaml -e "cluster_name=${CLUSTER_NAME}" || true
cd ..

backend_args=(
  -backend-config="bucket=${TF_STATE_BUCKET}"
  -backend-config="prefix=clusters/${CLUSTER_NAME}/state"
)
destroy_args=(
  -auto-approve
  -input=false
  -var="project_id=${PROJECT_ID}"
  -var="cluster_name=${CLUSTER_NAME}"
)
if [[ -n "${PROVISIONING_SA_EMAIL}" ]]; then
  backend_args+=(-backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}")
  destroy_args+=(-var="provisioning_sa_email=${PROVISIONING_SA_EMAIL}")
  export GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="${PROVISIONING_SA_EMAIL}"
fi
terraform -chdir=terraform/cluster init "${backend_args[@]}" || true
terraform -chdir=terraform/cluster destroy "${destroy_args[@]}" || true
