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

from pydantic import Field, field_validator

from gem_api.models.validators import (
    GcpTargetRequest,
    sanitize_optional_str,
    validate_gce_resource_name,
)


class EdgeRouterCreateRequest(GcpTargetRequest):
    """Request payload for building a GEM Edge Router."""

    edge_router_name: str = Field(
        default="gem-edge-router",
        description="GCE instance name for the edge router.",
    )
    machine_type: str = Field(
        default="e2-small",
        description="GCE VM machine type.",
    )
    gce_network: str = Field(
        default="gem-clusters-vpc",
        description="VPC network name.",
    )
    gce_subnetwork: str = Field(
        default="gem-clusters-subnet",
        description="Subnetwork name.",
    )

    @field_validator("edge_router_name")
    @classmethod
    def validate_edge_router_name(cls, v: str) -> str:
        return validate_gce_resource_name(v, "edge_router_name", max_len=63)


class EdgeRouterDeleteRequest(GcpTargetRequest):
    """Request payload for tearing down a GEM Edge Router."""

    edge_router_name: str = Field(
        default="gem-edge-router",
        description="GCE instance name for the edge router.",
    )
    tf_state_bucket: str | None = Field(
        default=None,
        description="GCS bucket holding remote state.",
    )

    @field_validator("edge_router_name")
    @classmethod
    def validate_edge_router_name(cls, v: str) -> str:
        return validate_gce_resource_name(v, "edge_router_name", max_len=63)

    @field_validator("tf_state_bucket", mode="before")
    @classmethod
    def sanitize_tf_state_bucket(cls, v: Any) -> Any:
        return sanitize_optional_str(v)
