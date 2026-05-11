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

locals {
  vms = {
    node1 = "${var.cluster_name}-node-1"
    node2 = "${var.cluster_name}-node-2"
    node3 = "${var.cluster_name}-node-3"
  }
}

data "google_compute_image" "ubuntu" {
  family  = "ubuntu-2404-lts-amd64"
  project = "ubuntu-os-cloud"
}

resource "google_compute_disk" "gdc_data_disks" {
  for_each = local.vms
  name     = "${each.value}-data"
  type     = "pd-ssd"
  zone     = var.zone
  size     = 1400
  project  = var.project_id
}

resource "google_compute_instance" "gdc_vms" {
  for_each     = local.vms
  name         = each.value
  machine_type = var.machine_type
  zone         = var.zone
  project      = var.project_id

  # Match GDCc Ice Lake CPU platform
  min_cpu_platform = "Intel Ice Lake"

  # Applies default GCP firewall rules to allow inbound traffic on ports 80 and 443
  tags = ["http-server", "https-server"]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
      size  = 100
      type  = "pd-ssd"
    }
  }

  attached_disk {
    source      = google_compute_disk.gdc_data_disks[each.key].id
    device_name = "data"
  }

  network_interface {
    network    = data.google_compute_network.gdc_vpc.self_link
    subnetwork = data.google_compute_subnetwork.gdc_subnet.self_link
  }

  can_ip_forward = true

  shielded_instance_config {
    enable_secure_boot          = false
    enable_vtpm                 = true
    enable_integrity_monitoring = true
  }

  advanced_machine_features {
    enable_nested_virtualization = true
  }

  metadata = {
    cluster_id     = var.cluster_name
    bmctl_version  = var.bmctl_version
    enable-oslogin = "FALSE"
    user-data      = <<-EOF
#cloud-config
bootcmd:
  # Initialize the secondary disk with a GPT label
  - parted -s /dev/disk/by-id/google-data mklabel gpt
  # Create a partition for node_storage (leaves the rest unpartitioned for cluster SDS)
  - parted -s /dev/disk/by-id/google-data mkpart node_storage ext4 0% ${var.node_storage_size}
runcmd:
  # Wait for the partition to populate in /dev
  - udevadm settle
  # Format and mount the node_storage partition (Partition 1 of the secondary disk)
  - mkfs.ext4 -F /dev/disk/by-id/google-data-part1
  - mkdir -p /mnt/node_storage
  - mount /dev/disk/by-id/google-data-part1 /mnt/node_storage
  - echo "UUID=$(blkid -s UUID -o value /dev/disk/by-id/google-data-part1) /mnt/node_storage ext4 defaults 0 2" >> /etc/fstab
EOF
  }

  service_account {
    scopes = ["cloud-platform"]
  }
}
