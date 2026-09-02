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

from typing import Any

from pydantic import BaseModel, Field, field_validator, model_validator

from gem_api.models.validators import (
    sanitize_optional_str,
    validate_email_or_sa,
    validate_project_id,
    validate_zone_and_region,
)


class WorkstationCreateRequest(BaseModel):
    """Request payload for building a GEM Admin Workstation."""

    project_id: str | None = Field(
        default=None,
        description="Target GCP Project ID. Defaults to environment project.",
    )
    zone: str | None = Field(
        default=None,
        description="Target GCP Zone. Defaults to environment zone.",
    )
    region: str | None = Field(
        default=None,
        description="Target GCP Region. Defaults to derived zone region.",
    )
    provisioning_sa_email: str | None = Field(
        default=None,
        description="Service account email to impersonate for Terraform provisioning.",
    )
    gce_network: str = Field(
        default="gem-clusters-vpc",
        description="VPC network name.",
    )
    gce_subnetwork: str = Field(
        default="gem-clusters-subnet",
        description="Subnetwork name.",
    )

    @field_validator(
        "project_id",
        "zone",
        "region",
        "provisioning_sa_email",
        mode="before",
    )
    @classmethod
    def sanitize_optional_fields(cls, v: Any) -> Any:
        return sanitize_optional_str(v)

    @field_validator("provisioning_sa_email")
    @classmethod
    def validate_provisioning_sa(cls, v: str | None) -> str | None:
        return validate_email_or_sa(v, "provisioning_sa_email")

    @field_validator("project_id")
    @classmethod
    def validate_project(cls, v: str | None) -> str | None:
        return validate_project_id(v)

    @model_validator(mode="after")
    def validate_workstation_parameters(self) -> "WorkstationCreateRequest":
        self.zone, self.region = validate_zone_and_region(self.zone, self.region)
        return self


class WorkstationDeleteRequest(BaseModel):
    """Request payload for tearing down a GEM Admin Workstation."""

    project_id: str | None = Field(
        default=None,
        description="Target GCP Project ID. Defaults to environment project.",
    )
    zone: str | None = Field(
        default=None,
        description="Target GCP Zone. Defaults to environment zone.",
    )
    region: str | None = Field(
        default=None,
        description="Target GCP Region. Defaults to derived zone region.",
    )
    provisioning_sa_email: str | None = Field(
        default=None,
        description="Email of the Terraform provisioning SA to impersonate.",
    )
    tf_state_bucket: str | None = Field(
        default=None,
        description="GCS bucket holding remote state.",
    )

    @field_validator(
        "project_id",
        "zone",
        "region",
        "provisioning_sa_email",
        "tf_state_bucket",
        mode="before",
    )
    @classmethod
    def sanitize_optional_fields(cls, v: Any) -> Any:
        return sanitize_optional_str(v)

    @field_validator("provisioning_sa_email")
    @classmethod
    def validate_provisioning_sa(cls, v: str | None) -> str | None:
        return validate_email_or_sa(v, "provisioning_sa_email")

    @field_validator("project_id")
    @classmethod
    def validate_project(cls, v: str | None) -> str | None:
        return validate_project_id(v)

    @model_validator(mode="after")
    def validate_workstation_parameters(self) -> "WorkstationDeleteRequest":
        self.zone, self.region = validate_zone_and_region(self.zone, self.region)
        return self
