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
  project_id            = "test-project"
  region                = "us-central1"
  zone                  = "us-central1-a"
  ar_location           = "us-central1"
  provisioning_sa_email = "tf-provisioner@test-project.iam.gserviceaccount.com"
}

run "validate_cloudbuild_defaults" {
  command = plan

  assert {
    condition     = length(google_project_service.apis) == 3
    error_message = "Cloud Build module should enable 3 required APIs."
  }

  assert {
    condition     = google_service_account.builder.account_id == "gem-cluster-builder"
    error_message = "Builder service account should default to gem-cluster-builder."
  }

  assert {
    condition     = length(google_project_iam_member.builder_roles) == 6
    error_message = "Builder service account should be granted 6 project IAM roles."
  }

  assert {
    condition     = google_service_account_iam_member.builder_impersonates_provisioner.role == "roles/iam.serviceAccountTokenCreator" && google_service_account_iam_member.builder_impersonates_provisioner.service_account_id == "projects/test-project/serviceAccounts/tf-provisioner@test-project.iam.gserviceaccount.com"
    error_message = "Builder SA must be granted serviceAccountTokenCreator on the provisioning SA."
  }

  assert {
    condition     = google_secret_manager_secret.ssh.secret_id == "gem-cluster-builder-ssh-key"
    error_message = "SSH Secret Manager secret should default to gem-cluster-builder-ssh-key."
  }

  assert {
    condition     = google_artifact_registry_repository.gem.repository_id == "gem" && google_artifact_registry_repository.gem.format == "DOCKER" && google_artifact_registry_repository.gem.location == "us-central1"
    error_message = "Artifact Registry repository should be DOCKER format in ar_location."
  }

  assert {
    condition     = output.artifact_registry_repo == "us-central1-docker.pkg.dev/test-project/gem" && output.ssh_secret_name == "gem-cluster-builder-ssh-key"
    error_message = "Cloud Build outputs should match expected repository path and secret name."
  }
}
