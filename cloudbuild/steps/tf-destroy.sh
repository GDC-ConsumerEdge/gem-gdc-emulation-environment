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

backend_args=(
  -backend-config="bucket=${TF_STATE_BUCKET}"
  -backend-config="prefix=clusters/${CLUSTER_NAME}/state"
)
destroy_args=(
  -auto-approve
  -input=false
  -var="project_id=${PROJECT_ID}"
  -var="cluster_name=${CLUSTER_NAME}"
  -var="zone=${GEM_GCP_ZONE}"
)

if [[ -n "${PROVISIONING_SA_EMAIL}" ]]; then
  backend_args+=(-backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}")
  destroy_args+=(-var="provisioning_sa_email=${PROVISIONING_SA_EMAIL}")
  export GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="${PROVISIONING_SA_EMAIL}"
fi

echo "🔄 Initializing terraform/cluster for teardown..."
terraform -chdir=terraform/cluster init "${backend_args[@]}"

echo "🗑️  Destroying GCP infrastructure for cluster ${CLUSTER_NAME}..."
terraform -chdir=terraform/cluster destroy "${destroy_args[@]}"
