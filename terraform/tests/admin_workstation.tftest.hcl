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

variables {
  project_id = "test-project"
  region     = "us-central1"
  zone       = "us-central1-a"
}

run "validate_admin_workstation_defaults" {
  command = plan

  assert {
    condition     = google_compute_instance.admin_ws.name == "gem-admin-ws" && google_compute_instance.admin_ws.machine_type == "e2-standard-4"
    error_message = "Admin workstation should be named gem-admin-ws with e2-standard-4 machine type."
  }

  assert {
    condition     = google_compute_instance.admin_ws.can_ip_forward == true
    error_message = "Admin workstation must have can_ip_forward enabled."
  }

  assert {
    condition     = google_compute_instance.admin_ws.deletion_protection == false
    error_message = "Admin workstation deletion_protection should default to false."
  }

  assert {
    condition     = google_compute_instance.admin_ws.network_interface[0].network_ip == "10.10.0.2"
    error_message = "Admin workstation IP should default to 10.10.0.2."
  }

  assert {
    condition     = google_compute_instance.admin_ws.shielded_instance_config[0].enable_secure_boot == true
    error_message = "Admin workstation should have secure boot enabled."
  }

  assert {
    condition     = google_compute_instance.admin_ws.metadata["enable-oslogin"] == "FALSE"
    error_message = "Admin workstation must disable OS Login."
  }

  assert {
    condition     = output.workstation_name == "gem-admin-ws" && output.workstation_ip == "10.10.0.2"
    error_message = "Outputs should expose workstation_name and workstation_ip."
  }
}

run "validate_admin_workstation_overrides" {
  command = plan

  variables {
    workstation_ip      = "10.10.0.10"
    deletion_protection = true
  }

  assert {
    condition     = google_compute_instance.admin_ws.network_interface[0].network_ip == "10.10.0.10"
    error_message = "Admin workstation should honor custom workstation_ip."
  }

  assert {
    condition     = google_compute_instance.admin_ws.deletion_protection == true
    error_message = "Admin workstation should honor deletion_protection override."
  }
}
