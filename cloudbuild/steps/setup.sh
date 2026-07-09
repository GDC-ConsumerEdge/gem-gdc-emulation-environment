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

# Resolves the state bucket default, persists resolved values for downstream
# steps, and writes the SSH key (from Secret Manager) + ssh config under
# /workspace/.ssh. Inputs arrive as env vars set by the Cloud Build step.

set -euo pipefail

TF_STATE_BUCKET="${TF_STATE_BUCKET_IN:-gem-${PROJECT_ID}-tfstate}"

# The build SA is intentionally under-privileged and it provisions new clusters by
# impersonating the provisioning SA. We default to the conventional tf-provisioner@
# identity when the caller didn't pass one explicitly.
PROVISIONING_SA_EMAIL="${PROVISIONING_SA_EMAIL:-tf-provisioner@${PROJECT_ID}.iam.gserviceaccount.com}"

mkdir -p /workspace/state
cat >/workspace/state/env <<ENV
export PROJECT_ID="${PROJECT_ID}"
export CLUSTER_NAME="${CLUSTER_NAME}"
export TF_STATE_BUCKET="${TF_STATE_BUCKET}"
export PROVISIONING_SA_EMAIL="${PROVISIONING_SA_EMAIL}"
export EMULATE_GDC_VERSION="${EMULATE_GDC_VERSION:-}"
export HARDWARE_VARIANT="${HARDWARE_VARIANT:-g2-medium}"
export DESTROY_ON_FAILURE="${DESTROY_ON_FAILURE:-false}"
ENV

echo "PROJECT_ID         = ${PROJECT_ID}"
echo "CLUSTER_NAME       = ${CLUSTER_NAME}"
echo "TF_STATE_BUCKET    = ${TF_STATE_BUCKET}"
echo "EMULATE_GDC_VERSION= ${EMULATE_GDC_VERSION:-<latest>}"
echo "HARDWARE_VARIANT   = ${HARDWARE_VARIANT:-g2-medium}"
echo "DESTROY_ON_FAILURE = ${DESTROY_ON_FAILURE:-false}"

# SSH key from Secret Manager -> ~/.ssh/google_compute_engine (the path the
# Ansible playbooks and ansible/ansible.cfg expect.
mkdir -p /workspace/.ssh
chmod 700 /workspace/.ssh
printf '%s' "${SSH_PRIVATE_KEY}" >/workspace/.ssh/google_compute_engine
chmod 600 /workspace/.ssh/google_compute_engine

# Disable strict host checking for direct ssh/scp invocations (bmctl, etc.)
cat >/workspace/.ssh/config <<'SSHCFG'
Host *
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    IdentityFile /workspace/.ssh/google_compute_engine
SSHCFG
chmod 600 /workspace/.ssh/config

gcloud config set project "${PROJECT_ID}" --quiet
gcloud config set compute/zone us-central1-a --quiet
