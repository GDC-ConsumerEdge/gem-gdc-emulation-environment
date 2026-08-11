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

set -euo pipefail

# Script to run unit tests for GEM (Terraform and Ansible)
# This script does not create actual infrastructure.

GEM_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo "🚀 GEM Root: $GEM_ROOT"

# Dependency Check
echo "Checking required tools"
missing=()
for cmd in terraform ansible-playbook; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        missing+=("$cmd")
    fi
done
if [ "${#missing[@]}" -gt 0 ]; then
    echo "❌ Missing required tools: ${missing[*]}" >&2
    exit 1
fi

# Terraform Unit Tests
echo "Running Terraform Unit Tests"
# Workaround for GCS backend: Copy cluster module to temp dir and remove backend.tf
TEMP_TF_DIR=$(mktemp -d)
trap 'rm -rf "${TEMP_TF_DIR:-}"' EXIT INT TERM

echo "Creating temp directory for Terraform tests: $TEMP_TF_DIR"
cp -r "$GEM_ROOT/terraform/cluster"/* "$TEMP_TF_DIR"/
rm -f "$TEMP_TF_DIR/backend.tf"

cd "$TEMP_TF_DIR"
echo "Initializing Terraform (local backend)..."
terraform init

echo "Copying test file..."
mkdir -p tests
cp "$GEM_ROOT/terraform/tests/unit.tftest.hcl" tests/

echo "Running terraform test..."
terraform test

# Ansible Unit Tests
echo "Running Ansible Unit Tests"
cd "$GEM_ROOT/ansible"
echo "Running template rendering test..."
ansible-playbook tests/test_gdc_template.yaml
echo "Running parameter validation check test..."
ansible-playbook tests/test_validations.yaml
echo "Running dynamic VXLAN & VLAN interface rendering test..."
ansible-playbook tests/test_vxlan_rendering.yaml

# Go Operator Unit Tests
if [ -d "$GEM_ROOT/operators/gem-nf-operator" ] && command -v go >/dev/null 2>&1; then
    echo "Running Go Operator Unit Tests..."
    (cd "$GEM_ROOT/operators/gem-nf-operator" && go test -v -cover ./...)
fi

echo "✅ All unit tests passed!"
