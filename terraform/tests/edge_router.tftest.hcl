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
    name = "gem-admin-ws"
    metadata = {
      workstation_pubkey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ edge@ws"
    }
  }
}

variables {
  project_id = "test-project"
  region     = "us-central1"
  zone       = "us-central1-a"
}

run "validate_edge_router_defaults" {
  command = plan

  assert {
    condition     = google_compute_instance.edge_router.name == "gem-edge-router" && google_compute_instance.edge_router.machine_type == "e2-small"
    error_message = "Edge router should default to gem-edge-router with e2-small machine type."
  }

  assert {
    condition     = google_compute_instance.edge_router.can_ip_forward == true
    error_message = "Edge router must have can_ip_forward enabled."
  }

  assert {
    condition     = google_compute_instance.edge_router.metadata["enable-oslogin"] == "FALSE"
    error_message = "Edge router must disable OS Login."
  }

  assert {
    condition     = google_compute_instance.edge_router.metadata["ssh-keys"] == "gem:ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ edge@ws"
    error_message = "Edge router should populate ssh-keys from gem-admin-ws metadata."
  }

  assert {
    condition     = output.edge_router_name == "gem-edge-router"
    error_message = "Output edge_router_name should match instance name."
  }
}

run "validate_edge_router_overrides" {
  command = plan

  variables {
    edge_router_name    = "custom-edge-router"
    machine_type        = "e2-medium"
    deletion_protection = true
  }

  assert {
    condition     = google_compute_instance.edge_router.name == "custom-edge-router" && google_compute_instance.edge_router.machine_type == "e2-medium"
    error_message = "Edge router should honor name and machine_type overrides."
  }

  assert {
    condition     = google_compute_instance.edge_router.deletion_protection == true
    error_message = "Edge router should honor deletion_protection override."
  }
}
