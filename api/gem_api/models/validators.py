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

import re
from typing import Any

GCP_ZONE_REGEX = re.compile(r"^[a-z]+-[a-z0-9]+-[a-z]$")
GCP_REGION_REGEX = re.compile(r"^[a-z]+-[a-z0-9]+$")
GCP_PROJECT_ID_REGEX = re.compile(r"^[a-z][a-z0-9-]*[a-z0-9]$")
STORAGE_SIZE_REGEX = re.compile(r"^[1-9][0-9]*(GB|TB|GiB|TiB)$")
EMAIL_REGEX = re.compile(r"^[a-zA-Z0-9_.+-]+@[a-zA-Z0-9-]+\.[a-zA-Z0-9-.]+$")


def sanitize_optional_str(val: Any) -> str | None:
    """Sanitize optional string, returning None if empty or Swagger placeholder."""
    if val is None:
        return None
    if not isinstance(val, str):
        return str(val)
    v = val.strip()
    if not v or v.lower() in ("string", "none", "null", "undefined"):
        return None
    return v


def validate_zone_and_region(
    zone: str | None, region: str | None
) -> tuple[str | None, str | None]:
    """Validate GCP zone and region syntax, cross-consistency, and auto-derivation.

    Raises ValueError with descriptive, user-friendly messages if invalid or swapped.
    Returns (validated_zone, validated_or_derived_region).
    """
    z = sanitize_optional_str(zone)
    r = sanitize_optional_str(region)

    # Detect swapped zone and region:
    # e.g., zone="us-east5" (which is a region) and region="us-east5-b" (which is a zone)
    if z and r and GCP_REGION_REGEX.match(z) and GCP_ZONE_REGEX.match(r):
        raise ValueError(
            f"Invalid GCP zone '{z}' and region '{r}': zone and region are swapped. "
            f"In GCP, '{z}' is a region and '{r}' is a zone. "
            f"Expected zone='{r}' and region='{z}'."
        )

    # Validate zone if provided
    if z is not None and not GCP_ZONE_REGEX.match(z):
        if GCP_REGION_REGEX.match(z):
            raise ValueError(
                f"Invalid GCP zone '{z}'. '{z}' is a GCP region; "
                f"zones must include a zone letter suffix such as '{z}-a' or '{z}-b'."
            )
        raise ValueError(
            f"Invalid GCP zone '{z}'. Zones must follow the format '<region>-<zone_letter>' "
            f"(e.g., 'us-central1-a', 'us-east5-b')."
        )

    # Validate region if provided
    if r is not None and not GCP_REGION_REGEX.match(r):
        if GCP_ZONE_REGEX.match(r):
            derived_r = r.rsplit("-", 1)[0]
            raise ValueError(
                f"Invalid GCP region '{r}'. '{r}' is a GCP zone; "
                f"regions must not include a zone letter suffix (e.g., '{derived_r}')."
            )
        raise ValueError(
            f"Invalid GCP region '{r}'. Regions must follow the format '<location>-<name>' "
            f"(e.g., 'us-central1', 'us-east5')."
        )

    # Cross-validate zone and region if both are provided
    if z is not None and r is not None:
        expected_region = z.rsplit("-", 1)[0]
        if r != expected_region:
            raise ValueError(
                f"Region '{r}' does not match zone '{z}' (expected region '{expected_region}')."
            )
    elif z is not None and r is None:
        # Automatically derive region from zone
        r = z.rsplit("-", 1)[0]

    return z, r


def validate_email_or_sa(val: str | None, field_name: str = "email") -> str | None:
    """Validate that a string conforms to service account email syntax."""
    v = sanitize_optional_str(val)
    if v is None:
        return None
    if not EMAIL_REGEX.match(v):
        raise ValueError(
            f"Invalid {field_name} '{v}'. Must be a valid service account email address."
        )
    return v


def validate_project_id(val: str | None) -> str | None:
    """Validate GCP project ID format."""
    v = sanitize_optional_str(val)
    if v is None:
        return None
    if not GCP_PROJECT_ID_REGEX.match(v):
        raise ValueError(
            f"Invalid project_id '{v}'. GCP project IDs must be lowercase alphanumeric with hyphens, "
            f"starting with a letter and ending with a letter or digit."
        )
    return v


def validate_storage_size(val: str) -> str:
    """Validate storage size format (e.g., '100GB', '1TB')."""
    v = val.strip()
    if not STORAGE_SIZE_REGEX.match(v):
        raise ValueError(
            f"Invalid node_storage_size '{v}'. Expected format like '100GB' or '1TB'."
        )
    return v
