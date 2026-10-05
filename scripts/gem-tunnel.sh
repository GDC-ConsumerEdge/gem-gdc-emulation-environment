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

# Open SSH tunnels to in-cluster services via the GEM Edge Router VM.
# The edge router sits on both the GCP VPC and the VXLAN overlays, so it reaches MetalLB
# LoadBalancer IPs and KubeVirt VM IPs.
# Kubernetes ClusterIPs are not yet supported

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: gem-tunnel.sh [options]

Forwarding flags:
  --http   <ip|ns/service>[:<port>][=<localport>] - Forward to an HTTP service
           (default remote port 80, local port 8080+)

  --rdp    <ip|ns/service>[:<port>][=<localport>] - Forward RDP to a VM
           (default remote port 3389, local port 13389+)

  --ssh    <ip|ns/service>[:<port>][=<localport>] - Forward SSH to a remote Service
           (default remote port 22, local port 2222+)

  --vnc    <ip|ns/service>[:<port>][=<localport>] - Forward VNC to a VM
           (default remote port 5900, local port 15900+)

  --tunnel <ip|ns/service>:<port>[=<localport>] - Forward to a remote IP or Service
           (default local port 9000+)

Connection overrides:
  --project-id <id>      GCP project ID (default: $PROJECT_ID or gcloud config get-value project)
  --edge-router <name>   GEM Edge Router VM name (default: gem-edge-router)
  --zone <zone>          GCP Zone where VMs reside (default: $GEM_GCP_ZONE, $CLOUDSDK_COMPUTE_ZONE, or gcloud config get-value compute/zone)
  --user <name>          SSH user (default: gem)
  --print                Print the ssh command instead of running it
  -h, --help             Show this help

Examples:
  # Tunnel to GEM MetalLB VIP 10.200.145.50 which is listening on TCP port 7523
  gem-tunnel.sh --http 10.200.145.50:7523

  # Print the gcloud command needed to tunnel to GEM MetalLB VIP 10.200.1.50
  gem-tunnel.sh --http 10.200.145.50 --print

  # Tunnel both RDP and VNC to two separate GEM MetalLB VIPs
  gem-tunnel.sh --rdp 10.200.145.51 --vnc 10.200.145.52

  # Tunnel to 10.200.145.50 on port 80, opening local port 8888
  gem-tunnel.sh --http 10.200.145.50:80=8888

USAGE
}

HTTP_SPECS=()
RDP_SPECS=()
SSH_SPECS=()
VNC_SPECS=()
TUNNEL_SPECS=()
PROJECT_ID="${PROJECT_ID:-}"
EDGE_NAME="${GEM_EDGE_ROUTER_NAME:-gem-edge-router}"
ZONE="${GEM_GCP_ZONE:-${CLOUDSDK_COMPUTE_ZONE:-}}"
SSH_USER="gem"
PRINT_ONLY=0

# Validate that we have the required options for a given argument
check_arg() {
  arg="$1" options="$2" val="${3:-}"
  if [[ "$options" -lt 2 || -z "$val" || "$val" == --* ]]; then
    echo -e "\n🚫 ERROR: $arg is missing a valid non-empty value.\n" >&2
    usage >&2
    exit 2
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
  --http)
    check_arg "$1" "$#" "${2:-}"
    HTTP_SPECS+=("$2")
    shift 2
    ;;
  --rdp)
    check_arg "$1" "$#" "${2:-}"
    RDP_SPECS+=("$2")
    shift 2
    ;;
  --ssh)
    check_arg "$1" "$#" "${2:-}"
    SSH_SPECS+=("$2")
    shift 2
    ;;
  --vnc)
    check_arg "$1" "$#" "${2:-}"
    VNC_SPECS+=("$2")
    shift 2
    ;;
  --tunnel)
    check_arg "$1" "$#" "${2:-}"
    TUNNEL_SPECS+=("$2")
    shift 2
    ;;
  --project-id)
    check_arg "$1" "$#" "${2:-}"
    PROJECT_ID="$2"
    shift 2
    ;;
  --edge-router)
    check_arg "$1" "$#" "${2:-}"
    EDGE_NAME="$2"
    shift 2
    ;;
  --zone)
    check_arg "$1" "$#" "${2:-}"
    ZONE="$2"
    shift 2
    ;;
  --user)
    check_arg "$1" "$#" "${2:-}"
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

if [[ ${#HTTP_SPECS[@]} -eq 0 \
  && ${#RDP_SPECS[@]} -eq 0 \
  && ${#SSH_SPECS[@]} -eq 0 \
  && ${#VNC_SPECS[@]} -eq 0 \
  && ${#TUNNEL_SPECS[@]} -eq 0 ]]; then
  echo -e "\n🚫 ERROR: must specify at least one of --http | --rdp | --ssh | --vnc | --tunnel\n" >&2
  usage >&2
  exit 2
fi

if ! command -v gcloud >/dev/null 2>&1; then
  echo "🚫 ERROR: missing required tool: gcloud" >&2
  exit 1
fi

if [[ -z "${PROJECT_ID:-}" ]]; then
  PROJECT_ID="$(gcloud config get-value project 2>/dev/null || true)"
fi
if [[ -z "$PROJECT_ID" ]]; then
  echo "🚫 ERROR: PROJECT_ID is not set and could not be read from gcloud" >&2
  exit 1
fi

if [[ -z "$EDGE_NAME" || "$EDGE_NAME" == "null" ]]; then
  echo "🚫 ERROR: GEM Edge Router not found.
   Ensure it has been deployed, or define with --edge-router" >&2
  exit 1
fi

if [[ -z "$ZONE" ]]; then
  ZONE="$(gcloud config get-value compute/zone 2>/dev/null || true)"
  if [[ -z "$ZONE"  ]]; then
    echo "🚫 ERROR: GCP zone not found.
   Define via --zone, GEM_GCP_ZONE, CLOUDSDK_COMPUTE_ZONE, or gcloud config set compute/zone <GCP zone>" >&2
    exit 1
  fi
fi

# Drive the connection through `gcloud compute ssh`  so it auto-provisions
# ~/.ssh/google_compute_engine and pushes the matching pubkey to project metadata for
# the requested target user.
GCLOUD_ARGS=(
  compute ssh "${SSH_USER}@${EDGE_NAME}"
  --zone="${ZONE}"
  --project="${PROJECT_ID}"
  --tunnel-through-iap
)
SSH_ARGS=("-N" "-o" "ControlMaster=no" "-o" "ControlPath=none")
SUMMARY=()

add_forward() {
  local_port="$1" remote_ip="$2" remote_port="$3" label="$4"
  SSH_ARGS+=("-L" "127.0.0.1:${local_port}:${remote_ip}:${remote_port}")

  # Define the url scheme (http, rdp, vnc, etc)
  scheme="$(echo "${label}" | tr '[:upper:]' '[:lower:]')"
  if [[ "$label" == "TUNNEL" ]]; then scheme="tcp"; fi

  # This outputs  HTTP:    http://localhost:8080 → 10.200.145.52:80
  printf -v formatted_line "%-8s %s://localhost:%s → %s:%s" "${label}:" "$scheme" "$local_port" "$remote_ip" "$remote_port"
  SUMMARY+=("$formatted_line")
}

parse_spec() {
  # Splits "<remote>[=<local_port>]" into REMOTE / LOCAL_PORT_OR_EMPTY.
  spec="$1"
  REMOTE="${spec%%=*}"
  if [[ "$spec" == *=* ]]; then
    LOCAL_PORT_OR_EMPTY="${spec#*=}"
  else
    LOCAL_PORT_OR_EMPTY=""
  fi
}

resolve_remote() {
  target="$1"
  if [[ "$target" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "$target"
    return 0
  fi

  if [[ "$target" != */* ]]; then
    echo -e "\n🚫 ERROR: Target '$target' is invalid. Endpoint must be specified as an
   IPv4 address or <namespace>/<service-name>\n" >&2
    exit 2
  fi

  ns="${target%%/*}"
  svc_name="${target##*/}"

  ip="$(kubectl get svc "$svc_name" -n "$ns" -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true)"
  if [[ -z "$ip" || "$ip" == "null" ]]; then
    echo -e "\n🚫 ERROR: Could not resolve LoadBalancer VIP for K8s service '$target' via kubectl.\n" >&2
    exit 2
  fi

  echo "$ip"
}

process_specs() {
  local label="$1" default_remote_port="$2" next_local_port="$3"
  shift 3
  local spec raw_ip remote_port remote_ip local_port
  for spec in "$@"; do
    parse_spec "$spec"
    if [[ "$REMOTE" == *:* ]]; then
      # Parameter expansion: "${REMOTE%:*}" strips the trailing ":<port>" to
      # extract the host/service; "${REMOTE##*:}" strips through the last ":"
      # to extract the port number.
      raw_ip="${REMOTE%:*}"
      remote_port="${REMOTE##*:}"
    elif [[ -n "$default_remote_port" ]]; then
      raw_ip="$REMOTE"
      remote_port="$default_remote_port"
    else
      echo "🚫 ERROR: --tunnel expects <ip|ns/service>:<port>[=<localport>], got '$spec'" >&2
      exit 2
    fi
    remote_ip="$(resolve_remote "$raw_ip")"
    # Use the explicit "=<localport>" if provided; otherwise assign the next
    # available default local port for this protocol and increment the counter.
    local_port="${LOCAL_PORT_OR_EMPTY:-$((next_local_port++))}"
    add_forward "$local_port" "$remote_ip" "$remote_port" "$label"
  done
}

# Translate any CLI specs collected for each protocol into SSH "-L" forwards.
[[ ${#HTTP_SPECS[@]} -gt 0 ]] && process_specs "HTTP" 80 8080 "${HTTP_SPECS[@]}"
[[ ${#RDP_SPECS[@]} -gt 0 ]] && process_specs "RDP" 3389 13389 "${RDP_SPECS[@]}"
[[ ${#SSH_SPECS[@]} -gt 0 ]] && process_specs "SSH" 22 2222 "${SSH_SPECS[@]}"
[[ ${#VNC_SPECS[@]} -gt 0 ]] && process_specs "VNC" 5900 15900 "${VNC_SPECS[@]}"
[[ ${#TUNNEL_SPECS[@]} -gt 0 ]] && process_specs "TUNNEL" "" 9000 "${TUNNEL_SPECS[@]}"

if [[ "$PRINT_ONLY" -eq 1 ]]; then
  # Just print the gcloud command needed to tunnel to a GEM Service
  printf 'gcloud'
  printf ' %q' "${GCLOUD_ARGS[@]}"
  printf ' --'
  printf ' %q' "${SSH_ARGS[@]}"
  printf '\n'
  exit 0
else
# Set up the tunnel, and print connection detail to the console
  echo -e '

              \ \        💎       \ \
 ______________\ \_________________\ \_______________
'
    echo ""
    for line in "${SUMMARY[@]}"; do echo " $line"; done
  echo '
 _______________  __________________  _______________
               / /                 / /
              / /                 / /


  Press Ctrl-C to disconnect
  '

  # exec'ing gcloud to capture SIGINT to clean up the openssh tunnel when the user Ctrl-Cs
  exec gcloud "${GCLOUD_ARGS[@]}" -- "${SSH_ARGS[@]}"
fi
