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

import os
from functools import lru_cache
from pathlib import Path
from typing import Any

import yaml

from gem_api.config import get_settings


@lru_cache
def load_group_vars() -> dict[str, Any]:
    """Load configuration from ansible/group_vars/all.yaml as the Single Source of Truth.

    Raises RuntimeError if the file cannot be located or parsed.
    """
    settings = get_settings()

    candidate_paths: list[Path] = []

    # Explicit environment variable override
    env_path = os.getenv("GEM_GROUP_VARS_PATH")
    if env_path:
        candidate_paths = [Path(env_path)]
    else:
        # Configured repo root
        candidate_paths.append(
            settings.repo_root / "ansible" / "group_vars" / "all.yaml"
        )

        # Standard container working directory (/app)
        candidate_paths.append(Path("/app/ansible/group_vars/all.yaml"))

        # Relative to current file location
        candidate_paths.append(
            Path(__file__).resolve().parent.parent.parent
            / "ansible"
            / "group_vars"
            / "all.yaml"
        )

    for path in candidate_paths:
        if path.is_file():
            try:
                with open(path, "r", encoding="utf-8") as f:
                    data = yaml.safe_load(f)
                    if isinstance(data, dict):
                        return data
            except Exception as e:
                raise RuntimeError(
                    f"Failed to parse Ansible group_vars at '{path}': {e}"
                ) from e

    searched_paths = "\n".join(f" - {p}" for p in candidate_paths)
    raise RuntimeError(
        "Could not locate 'ansible/group_vars/all.yaml'. "
        f"Searched candidate locations:\n{searched_paths}"
    )


def get_valid_gdc_versions() -> list[str]:
    """Retrieve supported GDC version strings defined in all.yaml."""
    data = load_group_vars()
    versions_map = data.get("emulated_gdc_versions")
    if not isinstance(versions_map, dict) or not versions_map:
        raise ValueError(
            "Missing or empty 'emulated_gdc_versions' in ansible/group_vars/all.yaml"
        )
    return list(versions_map.keys())


def get_default_gdc_version() -> str:
    """Retrieve default GDC version string from all.yaml."""
    versions = get_valid_gdc_versions()
    return versions[0]


def get_valid_hardware_variants() -> list[str]:
    """Retrieve supported hardware variant names defined in all.yaml."""
    data = load_group_vars()
    variants = data.get("hardware_variants")
    if not isinstance(variants, list) or not variants:
        raise ValueError(
            "Missing or empty 'hardware_variants' in ansible/group_vars/all.yaml"
        )
    return [str(v) for v in variants]


def get_default_hardware_variant() -> str:
    """Retrieve default hardware variant name from all.yaml."""
    data = load_group_vars()
    default_variant = data.get("default_hardware_variant")
    if default_variant and str(default_variant) in get_valid_hardware_variants():
        return str(default_variant)
    variants = get_valid_hardware_variants()
    return variants[0]


def get_default_secondary_networks() -> list[dict[str, Any]]:
    """Retrieve default secondary network definitions from all.yaml."""
    data = load_group_vars()
    networks = data.get("secondary_networks", [])
    if isinstance(networks, list):
        return networks
    return []
