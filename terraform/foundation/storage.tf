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

# GCS Bucket to sync vxlan network configurations between Admin Workstation and Edge Router
resource "google_storage_bucket" "overlay_sync" {
  name                        = "gem-${var.project_id}-overlay-sync"
  location                    = var.region
  project                     = var.project_id
  force_destroy               = true
  uniform_bucket_level_access = true
}

# Grant GCE Default Service Account admin access to this bucket
resource "google_storage_bucket_iam_member" "overlay_sync_accessor" {
  bucket = google_storage_bucket.overlay_sync.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${data.google_project.project.number}-compute@developer.gserviceaccount.com"
}
