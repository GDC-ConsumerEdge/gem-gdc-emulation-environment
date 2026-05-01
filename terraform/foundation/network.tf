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

resource "google_compute_network" "gdc_vpc" {
  name                    = var.gce_network
  project                 = var.project_id
  auto_create_subnetworks = false
  depends_on              = [google_project_service.apis]
}

resource "google_compute_subnetwork" "gdc_subnet" {
  name          = var.gce_subnetwork
  project       = var.project_id
  region        = var.region
  network       = google_compute_network.gdc_vpc.self_link
  ip_cidr_range = var.gce_subnetwork_cidr
}

resource "google_compute_firewall" "gdc_allow_internal" {
  name    = "gem-clusters-allow-internal"
  project = var.project_id
  network = google_compute_network.gdc_vpc.self_link

  allow {
    protocol = "tcp"
  }
  allow {
    protocol = "udp"
  }
  allow {
    protocol = "icmp"
  }

  source_ranges = [var.gce_subnetwork_cidr]
  # Targets VMs with these specific tags
  target_tags = ["http-server", "https-server"]
}

resource "google_compute_firewall" "gdc_allow_ssh" {
  name    = "gem-clusters-allow-iap-ssh"
  project = var.project_id
  network = google_compute_network.gdc_vpc.self_link

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = ["35.235.240.0/20"] # IAP Range
  # Targets VMs with these specific tags
  target_tags = ["http-server", "https-server"]
}

resource "google_compute_router" "router" {
  name    = "${var.gce_network}-router"
  region  = var.region
  network = google_compute_network.gdc_vpc.self_link
  project = var.project_id
}

resource "google_compute_router_nat" "nat" {
  name                               = "${var.gce_network}-nat"
  router                             = google_compute_router.router.name
  region                             = google_compute_router.router.region
  project                            = var.project_id
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"
}
