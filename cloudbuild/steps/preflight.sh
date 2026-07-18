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

conflict=0
for n in 1 2 3; do
  vm="${CLUSTER_NAME}-${n}"
  if gcloud compute instances describe "${vm}" --zone="${GEM_GCP_ZONE}" --format="value(name)" --quiet >/dev/null 2>&1; then
    echo "🚨 Pre-flight: VM ${vm} already exists in ${PROJECT_ID}."
    conflict=1
  fi
done

if gcloud container fleet memberships describe "${CLUSTER_NAME}" --location=global --quiet >/dev/null 2>&1; then
  echo "🚨 Pre-flight: fleet membership ${CLUSTER_NAME} already exists."
  conflict=1
fi

if [[ "${conflict}" -eq 1 ]]; then
  cat <<EOF
Refusing to start a new cluster build for ${CLUSTER_NAME}.

If this is leftover state from a previous failed build, clean it up:
  ansible-playbook ansible/cleanup.yaml -e cluster_name=${CLUSTER_NAME}
  terraform -chdir=terraform/cluster destroy -var=cluster_name=${CLUSTER_NAME}
EOF
  exit 1
fi

echo "✅ Pre-flight clean for ${CLUSTER_NAME}."
