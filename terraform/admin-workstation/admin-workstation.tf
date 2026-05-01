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

data "google_compute_image" "ubuntu" {
  family  = "ubuntu-2204-lts"
  project = "ubuntu-os-cloud"
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

resource "google_compute_instance" "admin_ws" {
  name         = "gem-admin-ws"
  machine_type = "e2-standard-4"
  zone         = var.zone
  project      = var.project_id

  can_ip_forward      = true
  deletion_protection = true

  # Applies default GCP firewall rules to allow inbound traffic on ports 80 and 443
  tags = ["http-server", "https-server"]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
      size  = 50
      type  = "pd-balanced"
    }
  }

  network_interface {
    network    = data.google_compute_network.gdc_vpc.self_link
    subnetwork = data.google_compute_subnetwork.gdc_subnet.self_link
  }

  shielded_instance_config {
    enable_secure_boot          = true
    enable_vtpm                 = true
    enable_integrity_monitoring = true
  }

  metadata = {
    enable-oslogin = "FALSE"
  }

  service_account {
    scopes = ["cloud-platform"]
  }
}
