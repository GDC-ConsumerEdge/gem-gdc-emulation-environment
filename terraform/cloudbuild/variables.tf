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

variable "project_id" {
  type = string
}

variable "region" {
  type = string

}

variable "zone" {
  type = string

}

variable "provisioning_sa_email" {
  type        = string
  description = "Email of the Terraform provisioning SA created by project-setup.sh. The builder SA is granted token-creator on this SA so Cloud Build can impersonate it for cluster provisioning."
}

variable "builder_sa_name" {
  type    = string
  default = "gem-cluster-builder"
}

variable "artifact_registry_repository" {
  type    = string
  default = "gem"
}

variable "ssh_secret_name" {
  type        = string
  description = "Secret Manager secret name that holds the workstation/cluster SSH private key. The workstation publishes the key on first provision; Cloud Build reads it at submit time."
  default     = "gem-cluster-builder-ssh-key"
}

variable "ar_location" {
  type        = string
  description = "Region where the Artifact Registry will be created"
}
