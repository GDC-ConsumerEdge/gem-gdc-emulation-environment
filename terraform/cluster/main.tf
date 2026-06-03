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

terraform {
  required_version = ">= 1.12.2"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.30.0"
    }
  }
}

provider "google" {
  project                     = var.project_id
  region                      = var.region
  zone                        = var.zone
  impersonate_service_account = var.provisioning_sa_email
}

# Read the shared Foundation networking
data "google_compute_network" "gdc_vpc" {
  name    = var.gce_network
  project = var.project_id
}

data "google_compute_subnetwork" "gdc_subnet" {
  name    = var.gce_subnetwork
  region  = var.region
  project = var.project_id
}

# Read the shared Admin Workstation
data "google_compute_instance" "gem_admin_ws" {
  name    = "gem-admin-ws"
  zone    = var.zone
  project = var.project_id
}
