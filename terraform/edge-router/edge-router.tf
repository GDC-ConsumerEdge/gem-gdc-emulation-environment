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
  required_version = ">= 1.16.6"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 8.5.0"
    }
  }
}

provider "google" {
  project                     = var.project_id
  region                      = var.region
  zone                        = var.zone
  impersonate_service_account = var.provisioning_sa_email
}

data "google_compute_network" "gdc_vpc" {
  name    = var.gce_network
  project = var.project_id
}

data "google_compute_subnetwork" "gdc_subnet" {
  name    = var.gce_subnetwork
  region  = var.region
  project = var.project_id
}

data "google_compute_image" "ubuntu" {
  family  = "ubuntu-2404-lts-amd64"
  project = "ubuntu-os-cloud"
}

data "google_storage_bucket_object_content" "admin_ws_state" {
  count  = var.workstation_zone == "" ? 1 : 0
  bucket = var.tf_state_bucket != "" ? var.tf_state_bucket : "gem-${var.project_id}-tfstate"
  name   = "admin-workstation/state/default.tfstate"
}

locals {
  admin_ws_state_raw = var.workstation_zone == "" ? try(jsondecode(data.google_storage_bucket_object_content.admin_ws_state[0].content), {}) : {}
  admin_ws_state_zone = coalesce(
    try(local.admin_ws_state_raw.outputs.zone.value, null),
    try([
      for r in local.admin_ws_state_raw.resources :
      r.instances[0].attributes.zone
      if r.type == "google_compute_instance" && length(r.instances) > 0
    ][0], null),
    var.zone
  )
  resolved_workstation_zone = var.workstation_zone != "" ? var.workstation_zone : local.admin_ws_state_zone
}

data "google_compute_instance" "gem_admin_ws" {
  name    = "gem-admin-ws"
  project = var.project_id
  zone    = local.resolved_workstation_zone

  lifecycle {
    postcondition {
      condition     = local.resolved_workstation_zone == var.zone || startswith(local.resolved_workstation_zone, "${var.region}-")
      error_message = "🚫 ERROR: gem-admin-ws is in zone '${local.resolved_workstation_zone}', which is outside target region '${var.region}'. The edge router must be deployed in the same GCP region as gem-admin-ws."
    }
  }
}

resource "google_compute_instance" "edge_router" {
  name         = var.edge_router_name
  machine_type = var.machine_type
  zone         = var.zone

  tags = ["http-server", "https-server"]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
      size  = 20
      type  = "pd-balanced"
    }
  }

  network_interface {
    network    = data.google_compute_network.gdc_vpc.self_link
    subnetwork = data.google_compute_subnetwork.gdc_subnet.self_link
  }

  can_ip_forward = true

  deletion_protection = var.deletion_protection

  metadata = {
    enable-oslogin = "FALSE"
    ssh-keys       = "gem:${lookup(data.google_compute_instance.gem_admin_ws.metadata, "workstation_pubkey", "")}"
  }

  service_account {
    scopes = ["cloud-platform"]
  }
}
