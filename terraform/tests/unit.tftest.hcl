// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

mock_provider "google" {}

override_data {
  target = data.google_compute_instance.gem_admin_ws
  values = {
    name = "mocked-gem-admin-ws"
    network_interface = [
      {
        network_ip = "10.0.0.100"
      }
    ]
  }
}

variables {
  project_id    = "test-project"
  region        = "us-central1"
  zone          = "us-central1-a"
  cluster_name  = "test-cluster"
  bmctl_version = "1.28.0"
}

run "validate_default_hardware_variant_g2_small_64gb" {
  command = plan

  assert {
    condition     = length(google_compute_instance.gdc_vms) == 3
    error_message = "Should provision exactly 3 VMs."
  }

  assert {
    condition     = google_compute_instance.gdc_vms["node1"].machine_type == "n4-custom-32-65536"
    error_message = "Default G2 Small 64GB should map to n4-custom-32-65536 machine type."
  }

  assert {
    condition     = google_compute_disk.gdc_data_disks["node1"].size == 3840
    error_message = "G2 Small 64GB should map to a 3840 GB data disk."
  }

  assert {
    condition     = google_compute_instance.gdc_vms["node1"].advanced_machine_features[0].enable_nested_virtualization == true
    error_message = "Nested virtualization should be enabled."
  }
}

run "validate_g2_small_128gb_hardware_variant" {
  command = plan

  variables {
    hardware_variant = "g2-small-128gb"
  }

  assert {
    condition     = google_compute_instance.gdc_vms["node1"].machine_type == "n4-standard-32"
    error_message = "G2 Small 128GB should map to n4-standard-32 machine type."
  }

  assert {
    condition     = google_compute_disk.gdc_data_disks["node1"].size == 3840
    error_message = "G2 Small 128GB should map to a 3840 GB data disk."
  }
}

run "validate_g2_medium_hardware_variant" {
  command = plan

  variables {
    hardware_variant = "g2-medium"
  }

  assert {
    condition     = google_compute_instance.gdc_vms["node1"].machine_type == "n4-custom-48-131072"
    error_message = "G2 Medium should map to n4-custom-48-131072 machine type."
  }

  assert {
    condition     = google_compute_disk.gdc_data_disks["node1"].size == 3840
    error_message = "G2 Medium should map to a 3840 GB data disk."
  }
}

run "validate_g1_medium_hardware_variant" {
  command = plan

  variables {
    hardware_variant = "g1-medium"
  }

  assert {
    condition     = google_compute_instance.gdc_vms["node1"].machine_type == "n2-custom-32-65536"
    error_message = "G1 Medium should map to n2-custom-32-65536 machine type."
  }

  assert {
    condition     = google_compute_disk.gdc_data_disks["node1"].size == 1600
    error_message = "G1 Medium should map to a 1600 GB data disk."
  }
}

run "validate_g1_large_hardware_variant" {
  command = plan

  variables {
    hardware_variant = "g1-large"
  }

  assert {
    condition     = google_compute_instance.gdc_vms["node1"].machine_type == "n2-custom-64-131072"
    error_message = "G1 Large should map to n2-custom-64-131072 machine type."
  }

  assert {
    condition     = google_compute_disk.gdc_data_disks["node1"].size == 3200
    error_message = "G1 Large should map to a 3200 GB data disk."
  }
}

run "validate_g2_large_hardware_variant" {
  command = plan

  variables {
    hardware_variant = "g2-large"
  }

  assert {
    condition     = google_compute_instance.gdc_vms["node1"].machine_type == "n4-custom-64-131072"
    error_message = "G2 Large should map to n4-custom-64-131072 machine type."
  }

  assert {
    condition     = google_compute_disk.gdc_data_disks["node1"].size == 3840
    error_message = "G2 Large should map to a 3840 GB data disk."
  }
}

run "validate_hardware_variant_validation_fail" {
  command = plan

  variables {
    hardware_variant = "invalid_choice_unknown"
  }

  expect_failures = [
    var.hardware_variant
  ]
}

run "validate_cluster_name_length_fail" {
  command = plan

  variables {
    cluster_name = "my-extremely-long-gem-cluster-name-which-fails-validation"
  }

  expect_failures = [
    var.cluster_name
  ]
}
