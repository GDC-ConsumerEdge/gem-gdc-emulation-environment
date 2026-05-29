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

# Artifact Registry repository that hosts the Cloud Build builder image used by the
# cluster-build pipeline.
resource "google_artifact_registry_repository" "gem" {
  repository_id = var.artifact_registry_repository
  location      = var.region
  format        = "DOCKER"
  description   = "GEM build artifacts (Cloud Build builder image)"
  project       = var.project_id
  depends_on    = [google_project_service.apis]
}
