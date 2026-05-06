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

# Open SSH tunnels to in-cluster services via the GEM edge router VM.
# The edge router sits on both the GCP VPC and the VXLAN overlays, so it
# reaches MetalLB LoadBalancer IPs and KubeVirt VM IPs. Kubernetes
# ClusterIPs (10.96.0.0/12) are kube-proxy-resolved and need a cluster
# node as the SSH hop instead — not supported by this script yet.

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: gem-tunnel.sh [options]

Forwarding flags:
  --http <ip>:<port>[=<localport>]    Forward an HTTP service     (default 8080+)
  --rdp  <ip>[=<localport>]           Forward RDP to a VM         (default 13389+)
  --vnc  <ip>[=<localport>]           Forward VNC to a VM         (default 15900+)

Other:
  --user <name>          SSH user (default: $USER)
  --print                Print the ssh command instead of running it
  -h, --help             Show this help

Environment:
  PROJECT_ID             GCP project ID. Read from gcloud config if not defined.
  GEM_EDGE_ROUTER_NAME   Skip TF-state lookup; use this edge router VM name
  GEM_ZONE               Skip TF-state lookup; use this zone

Examples:
  gem-tunnel.sh --http 10.200.1.50:80
  gem-tunnel.sh --rdp 10.200.5.10 --vnc 10.200.5.11
  gem-tunnel.sh --socks
  gem-tunnel.sh --http 10.200.1.50:80=8888 --print
USAGE
}

HTTP_SPECS=()
RDP_SPECS=()
VNC_SPECS=()
SSH_USER="${USER:-gem}"
PRINT_ONLY=0

while [[ $# -gt 0 ]]; do
  case "$1" in
  --http)
    HTTP_SPECS+=("$2")
    shift 2
    ;;
  --rdp)
    RDP_SPECS+=("$2")
    shift 2
    ;;
  --vnc)
    VNC_SPECS+=("$2")
    shift 2
    ;;
  --user)
    SSH_USER="$2"
    shift 2
    ;;
  --print)
    PRINT_ONLY=1
    shift
    ;;
  -h | --help)
    usage
    exit 0
    ;;
  *)
    echo "Unknown argument: $1" >&2
    usage >&2
    exit 2
    ;;
  esac
done

if [[ ${#HTTP_SPECS[@]} -eq 0 && ${#RDP_SPECS[@]} -eq 0 && ${#VNC_SPECS[@]} -eq 0 && -z "$SOCKS_PORT" ]]; then
  echo "ERROR: must specify at least one of --http/--rdp/--vnc/--socks" >&2
  usage >&2
  exit 2
fi

missing=()
for cmd in gcloud jq; do
  command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
done
if [[ ${#missing[@]} -gt 0 ]]; then
  echo "ERROR: missing required tools: ${missing[*]}" >&2
  exit 1
fi

if [[ -z "${PROJECT_ID:-}" ]]; then
  PROJECT_ID="$(gcloud config get-value project 2>/dev/null || true)"
fi
if [[ -z "$PROJECT_ID" ]]; then
  echo "ERROR: PROJECT_ID is not set and could not be read from gcloud" >&2
  exit 1
fi

EDGE_NAME="${GEM_EDGE_ROUTER_NAME:-}"
ZONE="${GEM_ZONE:-}"

if [[ -z "$EDGE_NAME" || -z "$ZONE" ]]; then
  BUCKET="gs://gem-${PROJECT_ID}-tfstate"
  STATE_DIR="$(mktemp -d)"
  trap 'rm -rf "$STATE_DIR"' EXIT INT TERM

  fetch_tf() {
    local prefix="$1" name="$2" module_dir="$3"
    local src="${BUCKET}/${prefix}/default.tfstate"
    gcloud storage cp "$src" "${STATE_DIR}/${name}.json" >/dev/null 2>&1 && return 0
    if ! gcloud storage ls "$src" >/dev/null 2>&1; then
      echo "ERROR: ${module_dir} has not been deployed (no Terraform state at ${src})." >&2
      echo "       Run 'cd ${module_dir} && terraform apply' first." >&2
    else
      echo "ERROR: cannot read ${src} (check bucket permissions and gcloud auth)." >&2
    fi
    exit 1
  }
  get_tf_output() {
    jq -r ".outputs.\"$2\".value" "${STATE_DIR}/$1.json"
  }

  [[ -z "$ZONE" ]] && {
    fetch_tf "admin-workstation/state" "admin-workstation" "terraform/admin-workstation"
    ZONE="$(get_tf_output admin-workstation zone)"
  }
  [[ -z "$EDGE_NAME" ]] && {
    fetch_tf "edge-router/state" "edge-router" "terraform/edge-router"
    EDGE_NAME="$(get_tf_output edge-router edge_router_name)"
  }
fi

if [[ -z "$EDGE_NAME" || "$EDGE_NAME" == "null" ]]; then
  echo "ERROR: edge router name not found — has terraform/edge-router been applied?" >&2
  exit 1
fi
if [[ -z "$ZONE" || "$ZONE" == "null" ]]; then
  echo "ERROR: zone not found — has terraform/admin-workstation been applied?" >&2
  exit 1
fi

# Drive the connection through `gcloud compute ssh` (not raw ssh) so it
# auto-provisions ~/.ssh/google_compute_engine and pushes the matching
# pubkey to project metadata for the requested target user. Raw ssh
# fails with "Permission denied (publickey)" the first time a new user
# (e.g., --user gem) is targeted because the key has never been pushed
# under that username.
GCLOUD_ARGS=(
  compute ssh "${SSH_USER}@${EDGE_NAME}"
  --zone="${ZONE}"
  --project="${PROJECT_ID}"
  --tunnel-through-iap
  --ssh-flag=-N
)
SUMMARY=()

add_forward() {
  local lport="$1" rip="$2" rport="$3" label="$4"
  GCLOUD_ARGS+=(--ssh-flag="-L ${lport}:${rip}:${rport}")
  SUMMARY+=("${label}: localhost:${lport} → ${rip}:${rport}")
}

parse_spec() {
  # Splits "<remote>[=<localport>]" into REMOTE / LPORT_OR_EMPTY globals.
  local spec="$1"
  REMOTE="${spec%%=*}"
  if [[ "$spec" == *=* ]]; then
    LPORT_OR_EMPTY="${spec#*=}"
  else
    LPORT_OR_EMPTY=""
  fi
}

http_next=8080
if [[ ${#HTTP_SPECS[@]} -gt 0 ]]; then
  for spec in "${HTTP_SPECS[@]}"; do
    parse_spec "$spec"
    if [[ "$REMOTE" != *:* ]]; then
      echo "ERROR: --http expects <ip>:<port>[=<localport>], got '$spec'" >&2
      exit 2
    fi
    rip="${REMOTE%:*}"
    rport="${REMOTE##*:}"
    lport="${LPORT_OR_EMPTY:-$((http_next++))}"
    add_forward "$lport" "$rip" "$rport" "HTTP"
  done
fi

rdp_next=13389
if [[ ${#RDP_SPECS[@]} -gt 0 ]]; then
  for spec in "${RDP_SPECS[@]}"; do
    parse_spec "$spec"
    lport="${LPORT_OR_EMPTY:-$((rdp_next++))}"
    add_forward "$lport" "$REMOTE" 3389 "RDP"
  done
fi

vnc_next=15900
if [[ ${#VNC_SPECS[@]} -gt 0 ]]; then
  for spec in "${VNC_SPECS[@]}"; do
    parse_spec "$spec"
    lport="${LPORT_OR_EMPTY:-$((vnc_next++))}"
    add_forward "$lport" "$REMOTE" 5900 "VNC"
  done
fi

if [[ -n "$SOCKS_PORT" ]]; then
  GCLOUD_ARGS+=(--ssh-flag="-D ${SOCKS_PORT}")
  SUMMARY+=("SOCKS5: localhost:${SOCKS_PORT}")
fi

echo "Edge router: ${EDGE_NAME} (zone ${ZONE}, project ${PROJECT_ID})"
for line in "${SUMMARY[@]}"; do echo "  $line"; done

if [[ "$PRINT_ONLY" -eq 1 ]]; then
  printf 'gcloud'
  printf ' %q' "${GCLOUD_ARGS[@]}"
  printf '\n'
  exit 0
fi

echo "Press Ctrl-C to disconnect."
exec gcloud "${GCLOUD_ARGS[@]}"
