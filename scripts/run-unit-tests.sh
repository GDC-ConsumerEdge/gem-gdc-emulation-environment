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

# Script to run unit tests for GEM (Terraform, Ansible, Go Operator, and Python API)
# This script does not create actual infrastructure.

GEM_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo "🚀 GEM Root: $GEM_ROOT"

RUN_TERRAFORM=false
RUN_ANSIBLE=false
RUN_GO=false
RUN_PYTHON=false

if [ $# -eq 0 ]; then
    RUN_TERRAFORM=true
    RUN_ANSIBLE=true
    RUN_GO=true
    RUN_PYTHON=true
else
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --terraform)
                RUN_TERRAFORM=true
                shift
                ;;
            --ansible)
                RUN_ANSIBLE=true
                shift
                ;;
            --go|--golang)
                RUN_GO=true
                shift
                ;;
            --python)
                RUN_PYTHON=true
                shift
                ;;
            --all)
                RUN_TERRAFORM=true
                RUN_ANSIBLE=true
                RUN_GO=true
                RUN_PYTHON=true
                shift
                ;;
            -h|--help)
                echo "Usage: $0 [OPTIONS]"
                echo "Options:"
                echo "  --terraform     Run Terraform unit tests"
                echo "  --ansible       Run Ansible unit tests"
                echo "  --go, --golang  Run Go operator unit tests"
                echo "  --python        Run Python API unit tests"
                echo "  --all           Run all unit tests (default if no options specified)"
                echo "  -h, --help      Display this help message"
                exit 0
                ;;
            *)
                echo "❌ Unknown option: $1" >&2
                echo "Run '$0 --help' for usage." >&2
                exit 1
                ;;
        esac
    done
fi

# Granular dependency check
check_tool() {
    local cmd="$1"
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "❌ Missing required tool: $cmd" >&2
        exit 1
    fi
}

echo "Checking required tools for selected test suites..."
if [ "$RUN_TERRAFORM" = true ]; then
    check_tool terraform
fi
if [ "$RUN_ANSIBLE" = true ]; then
    check_tool ansible-playbook
fi
if [ "$RUN_GO" = true ]; then
    check_tool go
fi
if [ "$RUN_PYTHON" = true ]; then
    check_tool uv
fi

# Terraform Unit Tests
if [ "$RUN_TERRAFORM" = true ]; then
    echo "Running Terraform Unit Tests..."
    TEMP_TF_DIR=$(mktemp -d)
    (
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
    )
    rm -rf "$TEMP_TF_DIR"
fi

# Ansible Unit Tests
if [ "$RUN_ANSIBLE" = true ]; then
    echo "Running Ansible Unit Tests..."
    (
        cd "$GEM_ROOT/ansible"
        echo "Running template rendering test..."
        ansible-playbook tests/test_gdc_template.yaml
        echo "Running parameter validation check test..."
        ansible-playbook tests/test_validations.yaml
        echo "Running dynamic VXLAN & VLAN interface rendering test..."
        ansible-playbook tests/test_vxlan_rendering.yaml
    )
fi

# Go Operator Unit Tests
if [ "$RUN_GO" = true ]; then
    if [ -d "$GEM_ROOT/operators/gem-network-operator" ]; then
        echo "Running Go Operator Unit Tests..."
        (cd "$GEM_ROOT/operators/gem-network-operator" && go test -v -cover ./...)
    fi
fi

# Python API Unit Tests
if [ "$RUN_PYTHON" = true ]; then
    if [ -d "$GEM_ROOT/api" ]; then
        echo "Running Python API Unit Tests..."
        (cd "$GEM_ROOT/api" && uv run pytest -v --cov=gem_api)
    fi
fi

echo "✅ All selected unit tests passed!"
