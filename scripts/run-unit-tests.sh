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
    TEMP_TF_ROOT=$(mktemp -d)
    (
        trap 'rm -rf "${TEMP_TF_ROOT:-}"' EXIT INT TERM
        export TF_PLUGIN_CACHE_DIR="$TEMP_TF_ROOT/plugin-cache"
        mkdir -p "$TF_PLUGIN_CACHE_DIR"

        declare -A TF_MODULE_TESTS=(
            ["foundation"]="foundation.tftest.hcl"
            ["admin-workstation"]="admin_workstation.tftest.hcl"
            ["cluster"]="unit.tftest.hcl"
            ["edge-router"]="edge_router.tftest.hcl"
            ["cloudbuild"]="cloudbuild.tftest.hcl"
        )

        for module in foundation admin-workstation cluster edge-router cloudbuild; do
            test_file="${TF_MODULE_TESTS[$module]}"
            mod_dir="$TEMP_TF_ROOT/$module"
            mkdir -p "$mod_dir/tests"
            cp -r "$GEM_ROOT/terraform/$module"/* "$mod_dir"/
            rm -f "$mod_dir/backend.tf"
            cp "$GEM_ROOT/terraform/tests/$test_file" "$mod_dir/tests/"

            echo "Testing Terraform module: $module ($test_file)..."
            (
                cd "$mod_dir"
                terraform init -backend=false >/dev/null
                terraform test
            )
        done
    )
    rm -rf "$TEMP_TF_ROOT"
fi

# Ansible Unit Tests
if [ "$RUN_ANSIBLE" = true ]; then
    echo "Running Ansible Unit Tests..."
    (
        cd "$GEM_ROOT/ansible"
        echo "Running template rendering test..."
        ansible-playbook -i localhost, -c local tests/test_gdc_template.yaml
        echo "Running parameter validation check test..."
        ansible-playbook -i localhost, -c local tests/test_validations.yaml
        echo "Running dynamic VXLAN & VLAN interface rendering test..."
        ansible-playbook -i localhost, -c local tests/test_vxlan_rendering.yaml
        echo "Running component templates and script rendering test..."
        ansible-playbook -i localhost, -c local tests/test_component_templates.yaml
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
        # --frozen runs against the locked dependencies and stops uv rewriting
        # api/uv.lock when tests run
        (cd "$GEM_ROOT/api" && uv run --frozen pytest -v --cov=gem_api)
    fi
fi

echo "✅ All selected unit tests passed!"
