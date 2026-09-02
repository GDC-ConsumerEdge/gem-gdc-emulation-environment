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

import pytest

from gem_api.models.validators import (
    sanitize_optional_str,
    validate_email_or_sa,
    validate_project_id,
    validate_storage_size,
    validate_zone_and_region,
)
from gem_api.services.runner import _resolve_zone_and_region


def test_sanitize_optional_str():
    assert sanitize_optional_str(None) is None
    assert sanitize_optional_str("") is None
    assert sanitize_optional_str("   ") is None
    assert sanitize_optional_str("string") is None
    assert sanitize_optional_str("STRING") is None
    assert sanitize_optional_str("null") is None
    assert sanitize_optional_str("none") is None
    assert sanitize_optional_str("undefined") is None
    assert sanitize_optional_str("  valid-value  ") == "valid-value"
    assert sanitize_optional_str(123) == "123"


def test_validate_zone_and_region_none():
    z, r = validate_zone_and_region(None, None)
    assert z is None
    assert r is None


def test_validate_zone_and_region_auto_derive():
    z, r = validate_zone_and_region("us-east5-b", None)
    assert z == "us-east5-b"
    assert r == "us-east5"


def test_validate_zone_and_region_matching():
    z, r = validate_zone_and_region("us-east5-b", "us-east5")
    assert z == "us-east5-b"
    assert r == "us-east5"


def test_validate_zone_and_region_swapped():
    with pytest.raises(ValueError, match="zone and region are swapped"):
        validate_zone_and_region("us-east5", "us-east5-b")


def test_validate_zone_as_region():
    with pytest.raises(ValueError, match="is a GCP region"):
        validate_zone_and_region("us-central1", None)


def test_validate_region_as_zone():
    with pytest.raises(ValueError, match="is a GCP zone"):
        validate_zone_and_region(None, "us-central1-a")


def test_validate_zone_invalid_format():
    with pytest.raises(ValueError, match="Invalid GCP zone"):
        validate_zone_and_region("invalid_zone", None)


def test_validate_region_invalid_format():
    with pytest.raises(ValueError, match="Invalid GCP region"):
        validate_zone_and_region(None, "invalid_region")


def test_validate_zone_and_region_mismatch():
    with pytest.raises(ValueError, match="does not match zone"):
        validate_zone_and_region("us-central1-a", "us-east5")


def test_validate_email_or_sa():
    assert validate_email_or_sa(None) is None
    assert validate_email_or_sa("string") is None
    assert validate_email_or_sa("admin@example.iam.gserviceaccount.com") == (
        "admin@example.iam.gserviceaccount.com"
    )
    with pytest.raises(ValueError, match="Must be a valid service account email"):
        validate_email_or_sa("not-an-email")


def test_validate_project_id():
    assert validate_project_id(None) is None
    assert validate_project_id("string") is None
    assert validate_project_id("my-gcp-project-123") == "my-gcp-project-123"
    with pytest.raises(ValueError, match="Invalid project_id"):
        validate_project_id("Invalid_Project_ID!")


def test_validate_storage_size():
    assert validate_storage_size("100GB") == "100GB"
    assert validate_storage_size("1TB") == "1TB"
    assert validate_storage_size("50GiB") == "50GiB"
    with pytest.raises(ValueError, match="Invalid node_storage_size"):
        validate_storage_size("100")
    with pytest.raises(ValueError, match="Invalid node_storage_size"):
        validate_storage_size("100MB")


def test_resolve_zone_and_region_defense_in_depth():
    # If swapped values somehow reach runner, runner auto-corrects them
    z, r = _resolve_zone_and_region(
        "us-east5", "us-east5-b", "us-central1-a", "us-central1"
    )
    assert z == "us-east5-b"
    assert r == "us-east5"

    # Default fallback
    z, r = _resolve_zone_and_region(None, None, "us-central1-a", "us-central1")
    assert z == "us-central1-a"
    assert r == "us-central1"

    # Derivation from zone
    z, r = _resolve_zone_and_region(
        "europe-west1-c", None, "us-central1-a", "us-central1"
    )
    assert z == "europe-west1-c"
    assert r == "europe-west1"
