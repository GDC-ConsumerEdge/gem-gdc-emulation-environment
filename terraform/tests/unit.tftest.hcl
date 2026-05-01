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
  machine_type  = "n1-standard-8"
  bmctl_version = "1.28.0"
}



run "validate_vm_count_and_config" {
  command = plan

  assert {
    condition     = length(google_compute_instance.gdc_vms) == 3
    error_message = "Should provision exactly 3 VMs."
  }

  assert {
    condition     = google_compute_instance.gdc_vms["node1"].machine_type == "n1-standard-8"
    error_message = "Workstation VM has wrong machine type."
  }

  assert {
    condition     = google_compute_instance.gdc_vms["node1"].advanced_machine_features[0].enable_nested_virtualization == true
    error_message = "Nested virtualization should be enabled."
  }
}
