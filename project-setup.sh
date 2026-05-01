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

# project-setup.sh
# Creates a dedicated service account for provisioning and grants it the necessary permissions.

set -euo pipefail

# Validate that all required environment variables are provided
REQUIRED_ENV_VARS=(
  "CLUSTER_NAME"
  "PROJECT_ID"
  "TF_STATE_BUCKET"
  "REPO_BASE"
  "PROVISIONING_SA_EMAIL"
  "IMPERSONATE_SA_EMAIL"
)

for var in "${REQUIRED_ENV_VARS[@]}"; do
  if [[ -z "${!var:-}" ]]; then
    echo "🚨 Environment variable $var is not set." >&2
    exit 1
  fi
done

echo "🔄 Enabling essential APIs..."
gcloud services enable cloudresourcemanager.googleapis.com serviceusage.googleapis.com iamcredentials.googleapis.com --project="${PROJECT_ID}"

# GCP APIs can sometimes take a moment to propagate globally after being enabled.
echo "⏳ Waiting for APIs to fully activate (10s)..."
sleep 10

PROVISIONING_SA_NAME=$(echo "${PROVISIONING_SA_EMAIL}" | cut -d@ -f1)

echo "🔄 Creating provisioning Service Account: ${PROVISIONING_SA_NAME}..."
gcloud iam service-accounts create "${PROVISIONING_SA_NAME}" \
  --display-name="Terraform Provisioning SA for GDCSO" \
  --project="${PROJECT_ID}" || true

echo "🔄 Granting roles to the provisioning Service Account..."
# Required roles for Terraform to manage infrastructure
ROLES=(
  "roles/editor"
  "roles/iam.serviceAccountAdmin"
  "roles/compute.admin"
  "roles/resourcemanager.projectIamAdmin"
  "roles/serviceusage.serviceUsageAdmin"
)

for role in "${ROLES[@]}"; do
  gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
    --member="serviceAccount:${PROVISIONING_SA_EMAIL}" \
    --role="${role}" \
    --condition=None \
    --no-user-output-enabled
done

# Allow your user account to impersonate this service account
USER_EMAIL=$(gcloud config get-value account)
echo "🔄 Allowing user ${USER_EMAIL} to impersonate ${PROVISIONING_SA_EMAIL}..."
gcloud iam service-accounts add-iam-policy-binding "${PROVISIONING_SA_EMAIL}" \
  --member="user:${USER_EMAIL}" \
  --role="roles/iam.serviceAccountTokenCreator" \
  --project="${PROJECT_ID}" \
  --no-user-output-enabled

echo "✅ Provisioning Service Account setup complete."

echo "🔄 Generating terraform.tfvars files..."
cat <<EOF > "${REPO_BASE}/terraform/foundation/terraform.tfvars"
project_id            = "${PROJECT_ID}"
EOF

cat <<EOF > "${REPO_BASE}/terraform/admin-workstation/terraform.tfvars"
project_id            = "${PROJECT_ID}"
EOF

cat <<EOF > "${REPO_BASE}/terraform/edge-router/terraform.tfvars"
project_id            = "${PROJECT_ID}"
EOF

cat <<EOF > "${REPO_BASE}/terraform/cluster/terraform.tfvars"
project_id            = "${PROJECT_ID}"
provisioning_sa_email = "${PROVISIONING_SA_EMAIL}"
cluster_name          = "${CLUSTER_NAME}"
EOF
echo "✅ terraform.tfvars created successfully."

echo "🔄 Creating Terraform state bucket..."
gcloud storage buckets create "gs://${TF_STATE_BUCKET}" --project="${PROJECT_ID}" --location="us-central1" || true
gcloud storage buckets update "gs://${TF_STATE_BUCKET}" --versioning || true

echo "🔄 Generating backend.tf files..."
cat <<EOF > "${REPO_BASE}/terraform/foundation/backend.tf"
terraform {
  backend "gcs" {}
}
EOF

cat <<EOF > "${REPO_BASE}/terraform/admin-workstation/backend.tf"
terraform {
  backend "gcs" {}
}
EOF

cat <<EOF > "${REPO_BASE}/terraform/edge-router/backend.tf"
terraform {
  backend "gcs" {}
}
EOF

cat <<EOF > "${REPO_BASE}/terraform/cluster/backend.tf"
terraform {
  backend "gcs" {}
}
EOF
echo "✅ backend.tf created successfully. Terraform will now use GCS for remote state"

echo "🚀 Bootstrap complete"
