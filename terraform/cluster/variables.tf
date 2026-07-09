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
  type    = string
  default = "us-central1"
}

variable "zone" {
  type    = string
  default = "us-central1-a"
}

# This variable is used by the Ansible playbooks
# tflint-ignore: terraform_unused_declarations
variable "provisioning_sa_email" {
  type    = string
  default = ""
}

variable "cluster_name" {
  type    = string
  default = "gem-cluster-1"
  validation {
    condition     = length(var.cluster_name) <= 26
    error_message = "🚨 ERROR: The cluster_name value must be 26 characters or fewer to prevent GCE VM hostnames from exceeding the strict 63-character Kubernetes metadata label limits."
  }
}

variable "bmctl_version" {
  type    = string
  default = "1.33.300-gke.60"
}

variable "hardware_variant" {
  type        = string
  description = "The target GDC hardware offering variant to emulate (e.g., g1-medium, g1-large, g2-small-64gb, g2-medium, g2-large)."
  default     = "g2-medium"
  validation {
    condition     = contains(["g1-medium", "g1-large", "g2-small-64gb", "g2-small-128gb", "g2-medium", "g2-large"], var.hardware_variant)
    error_message = "🚨 ERROR: The hardware_variant value must be one of: g1-medium, g1-large, g2-small-64gb, g2-small-128gb, g2-medium, g2-large."
  }
}

variable "gce_network" {
  type    = string
  default = "gem-clusters-vpc"
}

variable "gce_subnetwork" {
  type    = string
  default = "gem-clusters-subnet"
}

# tflint-ignore: terraform_unused_declarations
variable "gem_user" {
  type    = string
  default = "gdc"
}

# tflint-ignore: terraform_unused_declarations
variable "ssh_public_key" {
  type        = string
  description = "The public SSH key to add to the gdc user's authorized_keys."
  default     = ""
}

variable "node_storage_size" {
  type        = string
  description = "The size of the node storage partition (e.g., 100GB)."
  default     = "100GB"
}
