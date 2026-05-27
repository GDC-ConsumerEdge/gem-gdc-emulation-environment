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

# cloudbuild/setup.sh
# Thin wrapper that applies the terraform/cloudbuild module and prints the
# operator's remaining steps. All non-trivial state (APIs, SAs, IAM, AR repo,
# Secret Manager secret) is owned by Terraform; this script only chains the
# apply so a user doesn't have to remember the backend-config invocation.

set -euo pipefail

REQUIRED_ENV_VARS=(
  "PROJECT_ID"
  "TF_STATE_BUCKET"
  "PROVISIONING_SA_EMAIL"
)

for var in "${REQUIRED_ENV_VARS[@]}"; do
  if [[ -z "${!var:-}" ]]; then
    echo "🚨 Environment variable $var is not set." >&2
    exit 1
  fi
done

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"

echo "🔄 Initializing terraform/cloudbuild..."
terraform -chdir="${REPO_ROOT}/terraform/cloudbuild" init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=cloudbuild/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

echo "🔄 Applying terraform/cloudbuild..."
# Pass required vars explicitly so the wrapper works regardless of whether
# project-setup.sh has been re-run to generate terraform.tfvars.
terraform -chdir="${REPO_ROOT}/terraform/cloudbuild" apply -auto-approve \
  -var="project_id=${PROJECT_ID}" \
  -var="provisioning_sa_email=${PROVISIONING_SA_EMAIL}"

BUILDER_SA_EMAIL=$(terraform -chdir="${REPO_ROOT}/terraform/cloudbuild" output -raw builder_sa_email)
AR_REPO=$(terraform -chdir="${REPO_ROOT}/terraform/cloudbuild" output -raw artifact_registry_repo)
SSH_SECRET=$(terraform -chdir="${REPO_ROOT}/terraform/cloudbuild" output -raw ssh_secret_name)

cat <<EOF

✅ Cloud Build bootstrap complete.

Builder SA       : ${BUILDER_SA_EMAIL}
Artifact Registry: ${AR_REPO}
SSH Secret       : ${SSH_SECRET}  (empty container; populated by admin-workstation Ansible)

Next steps:

- Ensure you have provisioned the admin workstation:

     cd ${REPO_ROOT}/ansible
     ansible-playbook admin-workstation.yaml

- Build the Cloud Build, builder image. This is a one-time requirement, of when the
  Dockerfile changes:

     gcloud builds submit \\
       --config=${REPO_ROOT}/cloudbuild/builder/cloudbuild.yaml \\
       ${REPO_ROOT}/cloudbuild/builder

- Submit a cluster build:

     gcloud builds submit \\
       --config=${REPO_ROOT}/cloudbuild/cluster-build.cloudbuild.yaml \\
       --substitutions=_CLUSTER_NAME=\${CLUSTER_NAME} \\
       --service-account=projects/${PROJECT_ID}/serviceAccounts/${BUILDER_SA_EMAIL} \\
       ${REPO_ROOT}

EOF
