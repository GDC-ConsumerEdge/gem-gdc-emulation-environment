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
    node1 = "${var.cluster_name}-1"
    node2 = "${var.cluster_name}-2"
    node3 = "${var.cluster_name}-3"
  }
}

data "google_compute_image" "ubuntu" {
  family  = "ubuntu-2404-lts-amd64"
  project = "ubuntu-os-cloud"
}

resource "google_compute_disk" "gdc_data_disks" {
  for_each = local.vms
  name     = "${each.value}-data"
  type     = local.hardware_config.data_disk_type
  zone     = var.zone
  size     = local.hardware_config.data_disk_size
  project  = var.project_id
}

resource "google_compute_instance" "gdc_vms" {
  for_each     = local.vms
  name         = each.value
  machine_type = local.hardware_config.machine_type
  zone         = var.zone
  project      = var.project_id

  # Match GDCc hardware offering CPU platform
  min_cpu_platform = local.hardware_config.cpu_platform

  # Applies default GCP firewall rules to allow inbound traffic on ports 80 and 443
  tags = ["http-server", "https-server"]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
      size  = local.hardware_config.boot_disk_size
      type  = local.hardware_config.boot_disk_type
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
    # The admin workstation's SSH public key is managed natively via GCP Instance Metadata.
    # When the admin workstation is built, it's SSH public key is pushed into Compute Engine
    # metadata, and is retrieved by the cluster nodes here
    ssh-keys  = "gem:${lookup(data.google_compute_instance.gem_admin_ws.metadata, "workstation_pubkey", "")}"
    user-data = <<-EOF
#cloud-config
runcmd:
  # Partition and format the data disk on first boot only. This must not live in
  # bootcmd, which cloud-init runs on every boot: on later boots parted refuses
  # to relabel the disk while partition 1 is mounted, so cloud-init reports an
  # error on every boot, and if partition 1 is ever not mounted the relabel
  # succeeds and drops partition 2, the TopoLVM physical volume that the
  # cluster_nodes Ansible role adds after this. Every step is guarded so that
  # runcmd is safe to re-run, for example after an instance-id change.
  - |
    if ! parted -s /dev/disk/by-id/google-data print 2>/dev/null | grep -q node_storage; then
      parted -s /dev/disk/by-id/google-data mklabel gpt
      # The rest of the disk is left unpartitioned for cluster storage
      parted -s /dev/disk/by-id/google-data mkpart node_storage ext4 0% ${var.node_storage_size}
      udevadm settle
      mkfs.ext4 -F /dev/disk/by-id/google-data-part1
    fi
  - mkdir -p /mnt/node_storage
  - mountpoint -q /mnt/node_storage || mount /dev/disk/by-id/google-data-part1 /mnt/node_storage
  - grep -q ' /mnt/node_storage ' /etc/fstab || echo "UUID=$(blkid -s UUID -o value /dev/disk/by-id/google-data-part1) /mnt/node_storage ext4 defaults 0 2" >> /etc/fstab
EOF
  }

  service_account {
    scopes = ["cloud-platform"]
  }
}
