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

# This variable is used by the provider impersonation block and Ansible playbooks
variable "provisioning_sa_email" {
  type    = string
  default = ""
}

variable "cluster_name" {
  type    = string
  default = "gem-cluster-1"
  validation {
    condition     = length(var.cluster_name) <= 26 && (length(var.cluster_name) + length(var.zone) + length(var.project_id) + 15) <= 63
    error_message = "🚫 ERROR: The cluster_name value must be 26 characters or fewer and the combined node FQDN (<cluster_name>-<node>.<zone>.c.<project_id>.internal) must not exceed 63 characters."
  }
}

variable "bmctl_version" {
  type    = string
  default = "1.34.100-gke.97"
}

variable "hardware_variant" {
  type        = string
  description = "The target GDC hardware offering variant to emulate (see hardware-variants.tf for available options)."
  default     = "g2-small-64gb"
  validation {
    condition     = contains(keys(local.hardware_variants), var.hardware_variant)
    error_message = "🚫 ERROR: The hardware_variant value '${var.hardware_variant}' must be one of: ${join(", ", keys(local.hardware_variants))}."
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

variable "node_storage_size" {
  type        = string
  description = "The size of the node storage partition (e.g., 100GB)."
  default     = "100GB"
}
