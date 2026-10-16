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
  target = data.google_project.project
  values = {
    number = "123456789012"
  }
}

variables {
  project_id = "test-project"
  region     = "us-central1"
  zone       = "us-central1-a"
}

run "validate_foundation_defaults" {
  command = plan

  assert {
    condition     = google_compute_network.gdc_vpc.name == "gem-clusters-vpc" && google_compute_network.gdc_vpc.auto_create_subnetworks == false
    error_message = "VPC should default to gem-clusters-vpc with custom subnetworks."
  }

  assert {
    condition     = google_compute_subnetwork.gdc_subnet.name == "gem-clusters-subnet" && google_compute_subnetwork.gdc_subnet.ip_cidr_range == "10.10.0.0/24"
    error_message = "Subnetwork should default to gem-clusters-subnet with 10.10.0.0/24 CIDR."
  }

  assert {
    condition     = contains(google_compute_firewall.gdc_allow_ssh.source_ranges, "35.235.240.0/20")
    error_message = "SSH firewall rule should allow IAP source range 35.235.240.0/20."
  }

  assert {
    condition     = google_storage_bucket.overlay_sync.name == "gem-test-project-overlay-sync" && google_storage_bucket.overlay_sync.uniform_bucket_level_access == true
    error_message = "Overlay sync bucket should be named gem-test-project-overlay-sync with uniform bucket-level access."
  }

  assert {
    condition     = google_storage_bucket_iam_member.overlay_sync_accessor.member == "serviceAccount:123456789012-compute@developer.gserviceaccount.com"
    error_message = "Overlay sync bucket IAM member should grant access to the default compute service account."
  }

  assert {
    condition     = google_service_account.baremetal_gcr.account_id == "baremetal-gcr" && length(google_project_iam_member.baremetal_gcr_roles) == 10
    error_message = "baremetal-gcr SA should be created with 10 IAM roles."
  }

  assert {
    condition     = google_service_account.gem_cluster_admin.account_id == "gem-cluster-admin" && length(google_project_iam_member.gem_cluster_admin_roles) == 2
    error_message = "gem-cluster-admin SA should be created with 2 IAM roles."
  }

  assert {
    condition     = length(google_project_service.apis) == 20
    error_message = "Foundation module should enable 20 required project APIs."
  }

  assert {
    condition     = google_gke_hub_feature.configmanagement.name == "configmanagement"
    error_message = "GKE Hub configmanagement feature should be enabled."
  }

  assert {
    condition     = output.project_number == "123456789012" && output.overlay_sync_bucket_name == "gem-test-project-overlay-sync"
    error_message = "Foundation outputs should match expected project number and bucket name."
  }
}

run "validate_foundation_custom_network" {
  command = plan

  variables {
    gce_network         = "custom-gem-vpc"
    gce_subnetwork      = "custom-gem-subnet"
    gce_subnetwork_cidr = "10.20.0.0/24"
  }

  assert {
    condition     = google_compute_network.gdc_vpc.name == "custom-gem-vpc"
    error_message = "VPC should use overridden gce_network name."
  }

  assert {
    condition     = google_compute_subnetwork.gdc_subnet.name == "custom-gem-subnet" && google_compute_subnetwork.gdc_subnet.ip_cidr_range == "10.20.0.0/24"
    error_message = "Subnetwork should use overridden name and CIDR."
  }
}
