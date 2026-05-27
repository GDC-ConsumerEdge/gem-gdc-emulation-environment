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

output "builder_sa_email" {
  description = "Service account that Cloud Build runs as for the cluster-build pipeline."
  value       = google_service_account.builder.email
}

output "ssh_secret_name" {
  description = "Secret Manager secret name holding the workstation/cluster SSH private key."
  value       = google_secret_manager_secret.ssh.secret_id
}

output "ssh_secret_id" {
  description = "Fully qualified Secret Manager secret resource ID."
  value       = google_secret_manager_secret.ssh.id
}

output "artifact_registry_repo" {
  description = "Artifact Registry repository hosting the GEM builder image."
  value       = "${google_artifact_registry_repository.gem.location}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.gem.repository_id}"
}
