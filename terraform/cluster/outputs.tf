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

output "cluster_nodes_ips" {
  value = {
    node1 = google_compute_instance.gdc_vms["node1"].network_interface[0].network_ip
    node2 = google_compute_instance.gdc_vms["node2"].network_interface[0].network_ip
    node3 = google_compute_instance.gdc_vms["node3"].network_interface[0].network_ip
  }
}

output "cluster_nodes_names" {
  value = {
    node1 = google_compute_instance.gdc_vms["node1"].name
    node2 = google_compute_instance.gdc_vms["node2"].name
    node3 = google_compute_instance.gdc_vms["node3"].name
  }
}

output "workstation_name" {
  value = data.google_compute_instance.gem_admin_ws.name
}

output "workstation_ip" {
  value = data.google_compute_instance.gem_admin_ws.network_interface[0].network_ip
}

output "cluster_name" { value = var.cluster_name }
output "bmctl_version" { value = var.bmctl_version }
output "project_id" { value = var.project_id }
output "zone" { value = var.zone }
output "node_storage_size" { value = var.node_storage_size }
