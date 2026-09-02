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

if [[ ! "${GEM_GCP_ZONE:-}" =~ ^[a-z]+-[a-z0-9]+-[a-z]$ ]]; then
    echo "🚫 ERROR: Invalid or missing GEM_GCP_ZONE. Please provide a valid GCP zone (e.g. 'us-east1-a')." >&2
    exit 1
fi
GEM_GCP_REGION="${GEM_GCP_ZONE%-*}"
GEM_TF_STATE_LOCATION="${GEM_TF_STATE_LOCATION_IN:-$GEM_GCP_REGION}"

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
export HARDWARE_VARIANT="${HARDWARE_VARIANT:-g2-small-64gb}"
export DESTROY_ON_FAILURE="${DESTROY_ON_FAILURE:-false}"
export GEM_GCP_ZONE="${GEM_GCP_ZONE}"
export GEM_GCP_REGION="${GEM_GCP_REGION}"
export GEM_TF_STATE_LOCATION="${GEM_TF_STATE_LOCATION}"
export TF_VAR_zone="${GEM_GCP_ZONE}"
export TF_VAR_region="${GEM_GCP_REGION}"
ENV

echo "PROJECT_ID         = ${PROJECT_ID}"
echo "CLUSTER_NAME       = ${CLUSTER_NAME}"
echo "TF_STATE_BUCKET    = ${TF_STATE_BUCKET}"
echo "EMULATE_GDC_VERSION= ${EMULATE_GDC_VERSION:-<latest>}"
echo "HARDWARE_VARIANT   = ${HARDWARE_VARIANT:-g2-small-64gb}"
echo "DESTROY_ON_FAILURE = ${DESTROY_ON_FAILURE:-false}"
echo "GEM_GCP_ZONE       = ${GEM_GCP_ZONE}"
echo "GEM_GCP_REGION     = ${GEM_GCP_REGION}"

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
    IdentitiesOnly yes
SSHCFG
chmod 600 /workspace/.ssh/config

gcloud config set project "${PROJECT_ID}" --quiet
gcloud config set compute/zone "${GEM_GCP_ZONE}" --quiet
