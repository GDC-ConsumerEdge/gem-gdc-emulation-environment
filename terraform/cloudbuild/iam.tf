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

# Service Account that Cloud Build runs as when executing the cluster-build
# pipeline. Heavy compute work is done by impersonating the existing
# provisioning SA (see google_service_account_iam_member below), so this SA
# only carries the minimum project-level grants the pipeline needs directly.
resource "google_service_account" "builder" {
  account_id   = var.builder_sa_name
  display_name = "GEM Cluster Build SA (Cloud Build)"
  project      = var.project_id
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "builder_roles" {
  for_each = toset([
    "roles/storage.objectAdmin",        # Terraform state in GCS
    "roles/iap.tunnelResourceAccessor", # IAP tunnel to workstation + nodes
    "roles/logging.logWriter",          # Custom SA requirement for Cloud Build
    "roles/artifactregistry.writer",    # Build and push the builder image
    "roles/compute.viewer",             # Pre-flight conflict checks on VMs
    "roles/gkehub.viewer",              # Pre-flight conflict checks on fleet
  ])
  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.builder.email}"
}

# Allow the builder SA to impersonate the provisioning SA. Terraform inside
# Cloud Build uses GOOGLE_IMPERSONATE_SERVICE_ACCOUNT to pick this up.
resource "google_service_account_iam_member" "builder_impersonates_provisioner" {
  service_account_id = "projects/${var.project_id}/serviceAccounts/${var.provisioning_sa_email}"
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_service_account.builder.email}"
}
