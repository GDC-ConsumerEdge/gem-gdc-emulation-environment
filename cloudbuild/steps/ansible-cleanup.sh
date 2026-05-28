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

# Inject SSH Keys for Ansible cleanup over IAP
install -m 700 -d /root/.ssh
install -m 600 /workspace/.ssh/google_compute_engine /root/.ssh/google_compute_engine
[[ -f /workspace/.ssh/config ]] && install -m 600 /workspace/.ssh/config /root/.ssh/config

cd ansible

# Best-effort cleanup (un-registers cluster from GKE Hub Fleet)
if ! ansible-playbook cleanup.yaml -e "cluster_name=${CLUSTER_NAME}" -i inventory.sh; then
  echo "⚠️  Ansible GKE Hub cleanup failed or bypassed; proceeding with infrastructure destruction..."
fi
