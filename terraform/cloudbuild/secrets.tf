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

# Secret Manager secret that holds the workstation/cluster SSH private key.
# Only the resource is managed by Terraform,  the admin-workstation Ansible role is
# responsible for adding the the ssh private key to Secrets Manager, so the private key
# material never enters Terraform state.
resource "google_secret_manager_secret" "ssh" {
  secret_id = var.ssh_secret_name
  project   = var.project_id

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

# Cloud Build worker reads the latest version at pipeline submit time.
resource "google_secret_manager_secret_iam_member" "builder_accessor" {
  project   = var.project_id
  secret_id = google_secret_manager_secret.ssh.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.builder.email}"
}
