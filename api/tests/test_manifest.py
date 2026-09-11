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

from pathlib import Path

import pytest

from gem_api.manifest import (
    get_default_gdc_version,
    get_default_hardware_variant,
    get_default_secondary_networks,
    get_valid_gdc_versions,
    get_valid_hardware_variants,
    load_group_vars,
)


def test_load_group_vars_success():
    load_group_vars.cache_clear()
    data = load_group_vars()
    assert isinstance(data, dict)
    assert "emulated_gdc_versions" in data
    assert "hardware_variants" in data


def test_valid_gdc_versions():
    versions = get_valid_gdc_versions()
    assert "1.13.0" in versions
    assert "1.12.1" in versions
    assert get_default_gdc_version() == "1.13.0"


def test_valid_hardware_variants():
    variants = get_valid_hardware_variants()
    assert "g2-small-64gb" in variants
    assert "dev-and-test" in variants
    assert get_default_hardware_variant() == "g2-small-64gb"


def test_default_secondary_networks():
    networks = get_default_secondary_networks()
    assert len(networks) >= 1
    assert any(n["name"] == "vlan-123" for n in networks)


def test_load_group_vars_missing_file_raises_error(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
):
    load_group_vars.cache_clear()
    # Point to a nonexistent path and temporary repo root
    monkeypatch.setenv(
        "GEM_GROUP_VARS_PATH", str(tmp_path / "nonexistent" / "all.yaml")
    )
    monkeypatch.setenv("REPO_ROOT", str(tmp_path / "nonexistent"))

    with pytest.raises(
        RuntimeError, match="Could not locate 'ansible/group_vars/all.yaml'"
    ):
        load_group_vars()

    load_group_vars.cache_clear()
