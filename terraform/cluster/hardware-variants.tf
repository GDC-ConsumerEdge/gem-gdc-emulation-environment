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
  # Definitive mappings for official GDC connected hardware offerings to GCE resources
  hardware_variants = {
    # G1 Hardware Variants
    g1-medium = {
      machine_type   = "n2-custom-32-65536" # 32 vCPU, 64GB RAM
      cpu_platform   = "Intel Ice Lake"
      data_disk_size = 1600 # 1.6 TB SSD
      boot_disk_size = 100
      boot_disk_type = "pd-ssd"
      data_disk_type = "pd-ssd"
    }
    g1-large = {
      machine_type   = "n2-custom-64-131072" # 64 vCPU, 128 GB RAM
      cpu_platform   = "Intel Ice Lake"
      data_disk_size = 3200 # 3.2 TB SSD
      boot_disk_size = 100
      boot_disk_type = "pd-ssd"
      data_disk_type = "pd-ssd"
    }

    # G2 Hardware Variants
    g2-small-64gb = {
      machine_type   = "n4-custom-32-65536" # 32 vCPU, 64 GB RAM
      cpu_platform   = "Intel Emerald Rapids"
      data_disk_size = 3840 # 3.84 TB SSD
      boot_disk_size = 100
      boot_disk_type = "hyperdisk-balanced"
      data_disk_type = "hyperdisk-balanced"
    }
    g2-small-128gb = {
      machine_type   = "n4-standard-32" # 32 vCPU, 128 GB RAM
      cpu_platform   = "Intel Emerald Rapids"
      data_disk_size = 3840 # 3.84 TB SSD
      boot_disk_size = 100
      boot_disk_type = "hyperdisk-balanced"
      data_disk_type = "hyperdisk-balanced"
    }
    g2-medium = {
      machine_type   = "n4-custom-48-131072" # 48 vCPU, 128 GB RAM
      cpu_platform   = "Intel Emerald Rapids"
      data_disk_size = 3840 # 3.84 TB SSD
      boot_disk_size = 100
      boot_disk_type = "hyperdisk-balanced"
      data_disk_type = "hyperdisk-balanced"
    }
    g2-large = {
      machine_type   = "n4-custom-64-131072" # 64 vCPU, 128 GB RAM
      cpu_platform   = "Intel Emerald Rapids"
      data_disk_size = 3840 # 3.84 TB SSD
      boot_disk_size = 100
      boot_disk_type = "hyperdisk-balanced"
      data_disk_type = "hyperdisk-balanced"
    }

    # Small, low cost, single developer GEM variant.
    dev-and-test = {
      machine_type   = "n4-standard-4" # 4 vCPU, 16 GB RAM
      cpu_platform   = "Intel Emerald Rapids"
      data_disk_size = 150 # 150 GiB SSD
      boot_disk_size = 100
      boot_disk_type = "hyperdisk-balanced"
      data_disk_type = "hyperdisk-balanced"
    }
  }

  selected_hardware_variant = contains(keys(local.hardware_variants), var.hardware_variant) ? var.hardware_variant : "g2-small-64gb"
  hardware_config           = local.hardware_variants[local.selected_hardware_variant]
}


resource "terraform_data" "hardware_variant_validation" {
  lifecycle {
    precondition {
      condition     = contains(keys(local.hardware_variants), var.hardware_variant)
      error_message = "🚨 ERROR: The hardware_variant value '${var.hardware_variant}' must be one of: ${join(", ", keys(local.hardware_variants))}."
    }
  }
}
