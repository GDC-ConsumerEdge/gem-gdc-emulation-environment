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

# terraform init + apply for the cluster module.
# If needed, records the failed stage to /workspace/state/failed-stage so the conditional
# teardown step can act on it

set -euo pipefail
# shellcheck source=/dev/null
source /workspace/state/env

backend_args=(
  -backend-config="bucket=${TF_STATE_BUCKET}"
  -backend-config="prefix=clusters/${CLUSTER_NAME}/state"
)
apply_args=(
  -auto-approve
  -input=false
  -var="project_id=${PROJECT_ID}"
  -var="cluster_name=${CLUSTER_NAME}"
)
if [[ -n "${PROVISIONING_SA_EMAIL}" ]]; then
  backend_args+=(-backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}")
  apply_args+=(-var="provisioning_sa_email=${PROVISIONING_SA_EMAIL}")
  export GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="${PROVISIONING_SA_EMAIL}"
fi

if ! terraform -chdir=terraform/cluster init "${backend_args[@]}"; then
  echo "tf-init" >/workspace/state/failed-stage
  exit 1
fi
if ! terraform -chdir=terraform/cluster apply "${apply_args[@]}"; then
  echo "tf-apply" >/workspace/state/failed-stage
  exit 1
fi
