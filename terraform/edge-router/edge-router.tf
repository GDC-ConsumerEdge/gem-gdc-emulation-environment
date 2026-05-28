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
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

data "google_compute_network" "gdc_vpc" {
  name = "gem-clusters-vpc"
}

data "google_compute_subnetwork" "gdc_subnet" {
  name   = "gem-clusters-subnet"
  region = var.region
}

data "google_compute_image" "ubuntu" {
  family  = "ubuntu-2404-lts-amd64 "
  project = "ubuntu-os-cloud"
}

data "google_compute_instance" "gem_admin_ws" {
  name    = "gem-admin-ws"
  project = var.project_id
  zone    = var.zone
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

  deletion_protection = true

  metadata = {
    enable-oslogin = "FALSE"
    ssh-keys       = "gem:${lookup(data.google_compute_instance.gem_admin_ws.metadata, "workstation_pubkey", "")}"
  }

  service_account {
    scopes = ["cloud-platform"]
  }
}
