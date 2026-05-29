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


# The Ansible inventory references the SSH key as ~/.ssh/google_compute_engine.
# which is the running user (/root here) and /root is not persisted across Cloud Build
# steps,  so stage the key from the persisted /workspace dir into /root for this step.
install -m 700 -d /root/.ssh
install -m 600 /workspace/.ssh/google_compute_engine /root/.ssh/google_compute_engine
[[ -f /workspace/.ssh/config ]] && install -m 600 /workspace/.ssh/config /root/.ssh/config

if [[ -f /workspace/state/failed-stage ]]; then
  echo "Skipping Ansible,  earlier step failed ($(cat /workspace/state/failed-stage))."
  exit 0
fi

extra_vars=()
if [[ -n "${EMULATE_GDC_VERSION}" ]]; then
  extra_vars+=(--extra-vars "emulate_gdc_version=${EMULATE_GDC_VERSION}")
fi

cd ansible
if ! ansible-playbook create-cluster.yaml "${extra_vars[@]}"; then
  echo "ansible-create-cluster" >/workspace/state/failed-stage
  exit 1
fi
