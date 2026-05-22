#!/bin/bash
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

# Dynamic inventory script that aggregates Terraform outputs from the various GEM components.
# This script is called from Ansible, and uses the results as the dynamic inventory for
# the Ansible playbook run.

set -euo pipefail

# Ensure PROJECT_ID is set
if [ -z "${PROJECT_ID:-}" ]; then
  PROJECT_ID=$(gcloud config get-value project 2>/dev/null || echo "")
  if [ -z "$PROJECT_ID" ]; then
    echo "🚨 Environment variable PROJECT_ID is not set and could not be read from gcloud." >&2
    echo "{}"
    exit 1
  fi
fi

BUCKET="gs://gem-${PROJECT_ID}-tfstate"

# Temp directory for state files
STATE_DIR=$(mktemp -d)
trap 'rm -rf "$STATE_DIR"' EXIT

# Function to fetch state file
fetch_state() {
  local prefix=$1
  local name=$2
  # Terraform GCS backend stores state at <prefix>/default.tfstate
  gcloud storage cp "${BUCKET}/${prefix}/default.tfstate" "${STATE_DIR}/${name}.json" >/dev/null 2>&1 || touch "${STATE_DIR}/${name}.json"
}

# Fetch states
fetch_state "foundation/state" "foundation"
fetch_state "admin-workstation/state" "admin-workstation"
fetch_state "edge-router/state" "edge-router"

# We need CLUSTER_NAME to fetch cluster state.
# Check if it's in environment, else default.
CLUSTER_NAME="${CLUSTER_NAME:-gem-cluster-1}"
fetch_state "clusters/${CLUSTER_NAME}/state" "cluster"

# Helper function to get output from fetched state
get_tf_output() {
  local name=$1
  local key=$2
  local file="${STATE_DIR}/${name}.json"
  if [ -s "$file" ]; then
    local val
    val=$(jq -r ".outputs.\"$key\".value" "$file" 2>/dev/null)
    if [ "$val" == "null" ]; then
      echo ""
    else
      echo "$val"
    fi
  else
    echo ""
  fi
}

get_tf_json() {
  local name=$1
  local key=$2
  local json_key=$3
  local file="${STATE_DIR}/${name}.json"
  if [ -s "$file" ]; then
    local val
    val=$(jq -r ".outputs.\"$key\".value.\"$json_key\"" "$file" 2>/dev/null)
    if [ "$val" == "null" ]; then
      echo ""
    else
      echo "$val"
    fi
  else
    echo ""
  fi
}

# Fetch Admin Workstation details (Required)
GEM_WS_NAME=$(get_tf_output "admin-workstation" "workstation_name")
GEM_WS_INTERNAL_IP=$(get_tf_output "admin-workstation" "workstation_ip")
GCP_PROJECT=$(get_tf_output "admin-workstation" "project_id")
GCP_ZONE=$(get_tf_output "admin-workstation" "zone")

# Fallback if somehow missing
if [ -z "$GCP_PROJECT" ]; then
  GCP_PROJECT=$(get_tf_output "foundation" "project_id")
fi
GCP_PROJECT_NUMBER=$(get_tf_output "foundation" "project_number")

# Fetch Cluster details (Optional)
# shellcheck disable=SC2034
NODE1_NAME=$(get_tf_json "cluster" "cluster_nodes_names" "node1")
# shellcheck disable=SC2034
NODE2_NAME=$(get_tf_json "cluster" "cluster_nodes_names" "node2")
# shellcheck disable=SC2034
NODE3_NAME=$(get_tf_json "cluster" "cluster_nodes_names" "node3")

# shellcheck disable=SC2034
NODE1_INTERNAL_IP=$(get_tf_json "cluster" "cluster_nodes_ips" "node1")
# shellcheck disable=SC2034
NODE2_INTERNAL_IP=$(get_tf_json "cluster" "cluster_nodes_ips" "node2")
# shellcheck disable=SC2034
NODE3_INTERNAL_IP=$(get_tf_json "cluster" "cluster_nodes_ips" "node3")

# If CLUSTER_NAME was not set in env, we used the default to fetch the file.
# Let's see if the file contained a specific cluster_name output.
STORED_CLUSTER_NAME=$(get_tf_output "cluster" "cluster_name")
if [ -n "$STORED_CLUSTER_NAME" ]; then
  CLUSTER_NAME="$STORED_CLUSTER_NAME"
fi

BMCTL_VERSION=$(get_tf_output "cluster" "bmctl_version")
NODE_STORAGE_SIZE=$(get_tf_output "cluster" "node_storage_size")
if [ -z "$NODE_STORAGE_SIZE" ]; then
  NODE_STORAGE_SIZE="100GB"
fi

# Fetch Edge Router details (Optional)
# shellcheck disable=SC2034
EDGE_ROUTER_IP=$(get_tf_output "edge-router" "edge_router_ip")
EDGE_ROUTER_NAME=$(get_tf_output "edge-router" "edge_router_name")

# If Admin WS isn't deployed yet, return empty inventory
if [ -z "$GEM_WS_NAME" ]; then
  echo "{}"
  exit 0
fi

# Deterministic Hashing Scheme for Network Isolation (VNI & Subnets)
# To ensure multi-cluster network isolation without requiring centralized IPAM,
# we calculate a 32-bit CRC checksum of the cluster name using cksum.
# "gem-cluster-1" == 3901383111
# Then we modulo the hash, to map it into the valid 24-bit VXLAN Network Identifier (VNI)
# range of 100 - 16,000,100. The modulo of "gem-cluster-1" == 13383211
# This VXLAN_ID is used both as the kernel VNI header and a a portion of a systemd-networkd
# virtual interface name (e.g., vx-gemclu-1338).
HASH=$(echo -n "$CLUSTER_NAME" | cksum | awk '{print $1}')
VXLAN_ID=$(( HASH % 16000000 + 100 ))
OCTET3=$(( HASH % 254 + 1 ))
VXLAN_BASE="10.200.${OCTET3}"

GCP_USER="${USER:-$(whoami)}"

# Build JSON inventory
cat <<EOF
{
  "all": {
    "vars": {
      "ansible_ssh_common_args": "-i ~/.ssh/google_compute_engine -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ControlMaster=auto -o ControlPersist=30m -o ConnectionAttempts=100 -o ProxyCommand='gcloud compute start-iap-tunnel %h %p --listen-on-stdin --project=${GCP_PROJECT} --zone=${GCP_ZONE}'",
      "ansible_python_interpreter": "/usr/bin/python3",
      "ansible_user": "${GCP_USER}",
      "gcp_project_id": "${GCP_PROJECT}",
      "gcp_project_number": "${GCP_PROJECT_NUMBER}",
      "gcp_zone": "${GCP_ZONE}",
      "tf_cluster_name": "${CLUSTER_NAME}",
$(if [ -n "$BMCTL_VERSION" ]; then echo "      \"bmctl_version\": \"${BMCTL_VERSION}\","; fi)
      "vxlan_id": "${VXLAN_ID}",
      "vxlan_base_ip": "${VXLAN_BASE}",
      "node_storage_size": "${NODE_STORAGE_SIZE}"
    }
  },
  "workstation": {
    "hosts": ["gem_admin_ws"]
  },
  "cluster_nodes": {
    "hosts": $(if [ -n "$NODE1_NAME" ]; then echo "[\"node1\", \"node2\", \"node3\"]"; else echo "[]"; fi)
  },
  "edge_router": {
    "hosts": $(if [ -n "$EDGE_ROUTER_NAME" ]; then echo "[\"edge_router_host\"]"; else echo "[]"; fi)
  },
  "gdc_nodes": {
    "children": [
      "workstation"
      $(if [ -n "$NODE1_NAME" ]; then echo ', "cluster_nodes"'; fi)
      $(if [ -n "$EDGE_ROUTER_NAME" ]; then echo ', "edge_router"'; fi)
    ]
  },
  "_meta": {
    "hostvars": {
      "gem_admin_ws": {
        "ansible_host": "${GEM_WS_NAME}",
        "internal_ip": "${GEM_WS_INTERNAL_IP}",
        "vxlan_ip": "${VXLAN_BASE}.100",
        "host_octet": 100
      }
$(if [ -n "$EDGE_ROUTER_NAME" ]; then cat <<INNER_EOF
      , "edge_router_host": {
        "ansible_host": "${EDGE_ROUTER_NAME}",
        "internal_ip": "${EDGE_ROUTER_IP}",
        "vxlan_ip": "${VXLAN_BASE}.254",
        "host_octet": 254
      }
INNER_EOF
fi)
$(if [ -n "$NODE1_NAME" ]; then cat <<INNER_EOF
      , "node1": {
        "ansible_host": "${NODE1_NAME}",
        "internal_ip": "${NODE1_INTERNAL_IP}",
        "vxlan_ip": "${VXLAN_BASE}.2",
        "host_octet": 2
      },
      "node2": {
        "ansible_host": "${NODE2_NAME}",
        "internal_ip": "${NODE2_INTERNAL_IP}",
        "vxlan_ip": "${VXLAN_BASE}.3",
        "host_octet": 3
      },
      "node3": {
        "ansible_host": "${NODE3_NAME}",
        "internal_ip": "${NODE3_INTERNAL_IP}",
        "vxlan_ip": "${VXLAN_BASE}.4",
        "host_octet": 4
      }
INNER_EOF
fi)
    }
  }
}
EOF
