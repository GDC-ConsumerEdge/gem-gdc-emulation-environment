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

# GEM clusters require dynamic VXLAN overlay interfaces (vx-*, sec-*) which are configured
# on the Admin Workstation (ws) and Edge Router (er). We're storing these network configurations
# in GCS and on a schedule, sync the configurations down to the ws and er. This provides
# a single source of truth for these network configs (GCS), so that the ws and er can
# be ephemeral. If one of those VMs is rebuilt, all existing vxlan network configurations
# is synced down to that VM, ensuring they retain connectivity to the GEM clusters.

set -euo pipefail

# Ensure PROJECT_ID is available to the cron environment
if [[ -z "${PROJECT_ID:-}" ]]; then
  PROJECT_ID=$(gcloud config get-value project 2>/dev/null || echo "")
  if [[ -z "$PROJECT_ID" ]]; then
    exit 0
  fi
fi

HOST_DIR="${HOST_DIR:-}"
if [[ -n "$HOST_DIR" ]]; then
  BUCKET="gs://gem-${PROJECT_ID}-overlay-sync/${HOST_DIR}"
else
  BUCKET="gs://gem-${PROJECT_ID}-overlay-sync"
fi
STAGING_DIR="/tmp/gem-overlay-staging"
TARGET_DIR="/etc/systemd/network"

mkdir -p "${STAGING_DIR}"
chmod 700 "${STAGING_DIR}"

# Clear out local staging directory
rm -rf "${STAGING_DIR:?}"/*

# Sync GCS bucket to local temp folder
if gcloud storage rsync --delete-unmatched-destination-objects "${BUCKET}" "${STAGING_DIR}" >/dev/null 2>&1; then
  changed=0

  # Remove local files which are no longer present in GCS
  # The most likely situation where this happens is when a GEM cluster has been deleted
  # and we no longer need it's vxlan configurations
  for file in "${TARGET_DIR}"/10-vx-* "${TARGET_DIR}"/10-sec-*; do
    [[ ! -f "$file" ]] && continue
    filename=$(basename "$file")

    # If the file exists locally but was removed from GCS
    if [[ ! -f "${STAGING_DIR}/${filename}" ]]; then
      # Help to prevent a race condition where a newly templated file hasn't been uploaded
      # to GCS yet. Only delete files modified > 5 minutes ago.
      if [[ $(find "$file" -mmin +5 -print) ]]; then
        rm -f "$file"
        changed=1
      fi
    fi
  done

  # Process additions and updates
  for file in "${STAGING_DIR}"/*; do
    [[ ! -f "$file" ]] && continue
    filename=$(basename "$file")
    target_file="${TARGET_DIR}/${filename}"

    if [[ ! -f "${target_file}" ]] || ! cmp -s "$file" "${target_file}"; then
      cp "$file" "${target_file}"
      chmod 644 "${target_file}"
      chown root:root "${target_file}"
      changed=1
    fi
  done

  if [[ "${changed}" -eq 1 ]]; then
    echo "🔄 [$(date)] GEM Overlay Sync: Detected VXLAN changes reloading systemd-networkd..."
    systemctl restart systemd-networkd
  fi
fi

rm -rf "${STAGING_DIR}"
