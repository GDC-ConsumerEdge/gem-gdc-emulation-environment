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
  type        = string
  description = "The GCP project ID"
}

variable "provisioning_sa_email" {
  type        = string
  description = "Email of the Terraform provisioning SA to impersonate for resource operations. Empty disables impersonation and uses the caller's credentials."
  default     = ""
}

variable "region" {
  type = string

}

variable "zone" {
  type = string

}

variable "workstation_zone" {
  type        = string
  description = "GCP zone hosting gem-admin-ws. Auto-discovered from remote Terraform state when empty."
  default     = ""
  validation {
    condition     = var.workstation_zone == "" || (can(regex("^[a-z]+-[a-z0-9]+-[a-z]$", var.workstation_zone)) && startswith(var.workstation_zone, "${var.region}-"))
    error_message = "🚫 ERROR: The workstation_zone value must be a valid GCP zone within region '${var.region}' (e.g., '${var.region}-b'), or empty to auto-discover from state."
  }
}

variable "tf_state_bucket" {
  type        = string
  description = "GCS bucket holding GEM Terraform state. Defaults to gem-<project_id>-tfstate when empty."
  default     = ""
}

variable "machine_type" {
  type    = string
  default = "e2-small"
}

variable "edge_router_name" {
  type    = string
  default = "gem-edge-router"
}

variable "deletion_protection" {
  type        = bool
  description = "Whether to enable deletion protection on the instance."
  default     = false
}

variable "gce_network" {
  type        = string
  description = "The VPC network name"
  default     = "gem-clusters-vpc"
}

variable "gce_subnetwork" {
  type        = string
  description = "The subnetwork name"
  default     = "gem-clusters-subnet"
}
