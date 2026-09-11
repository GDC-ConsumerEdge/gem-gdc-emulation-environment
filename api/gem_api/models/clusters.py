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

import ipaddress
import re
from typing import Any, Literal

from pydantic import BaseModel, Field, field_validator, model_validator

from gem_api.manifest import (
    get_default_gdc_version,
    get_default_hardware_variant,
    get_valid_gdc_versions,
    get_valid_hardware_variants,
)
from gem_api.models.validators import (
    sanitize_optional_str,
    validate_email_or_sa,
    validate_project_id,
    validate_storage_size,
    validate_zone_and_region,
)


class SecondaryNetworkConfig(BaseModel):
    """Configuration definition for a secondary Multus L2 overlay network."""

    name: str = Field(..., description="Unique network name", examples=["vlan-123"])
    vlan_id: int = Field(
        ..., ge=1, le=4094, description="VLAN identifier (1-4094)", examples=[123]
    )
    subnet: str = Field(
        ..., description="IPv4 Subnet in CIDR notation", examples=["172.16.12.0/24"]
    )
    gateway: str = Field(
        ..., description="Default gateway IP for the subnet", examples=["172.16.12.1"]
    )
    vip_pool: str = Field(
        ...,
        description="MetalLB LoadBalancer VIP range",
        examples=["172.16.12.200-172.16.12.250"],
    )
    pod_cidr: str = Field(
        default="10.12.0.0/22",
        description="Pod CIDR block allocation",
        examples=["10.12.0.0/22"],
    )
    per_node_ipam_size: int = Field(
        default=24,
        description="Per-node IPAM size",
        examples=[24],
    )

    @field_validator("name")
    @classmethod
    def validate_name(cls, v: str) -> str:
        v = v.strip()
        if not v:
            raise ValueError("Secondary network name cannot be empty.")
        if not re.match(r"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$", v):
            raise ValueError(
                f"Secondary network name '{v}' must be a valid DNS-1123 label (lowercase alphanumeric and hyphens)."
            )
        return v

    @field_validator("subnet", "pod_cidr")
    @classmethod
    def validate_cidr(cls, v: str) -> str:
        v = v.strip()
        try:
            ipaddress.ip_network(v, strict=False)
        except ValueError:
            raise ValueError(f"'{v}' is not a valid IPv4 CIDR block.")
        return v

    @field_validator("gateway")
    @classmethod
    def validate_gateway(cls, v: str) -> str:
        v = v.strip()
        try:
            ipaddress.ip_address(v)
        except ValueError:
            raise ValueError(f"'{v}' is not a valid IPv4 address.")
        return v


class ClusterCreateRequest(BaseModel):
    """Request payload for building a new GEM cluster."""

    cluster_name: str = Field(
        default="gem-cluster-1",
        description="Unique identifier for the cluster (maximum 26 characters)",
        examples=["gem-cluster-1"],
    )
    project_id: str | None = Field(
        default=None,
        description="Target GCP Project ID. Defaults to configured environment project.",
    )
    zone: str | None = Field(
        default=None,
        description="Target GCP Zone (e.g., 'us-central1-a'). Defaults to configured zone.",
    )
    region: str | None = Field(
        default=None,
        description="Target GCP Region. Defaults to derived zone region.",
    )
    hardware_variant: str = Field(
        default_factory=get_default_hardware_variant,
        description="GDC hardware offering to emulate.",
        examples=["g2-small-64gb"],
    )
    emulate_gdc_version: str = Field(
        default_factory=get_default_gdc_version,
        description="GDC Connected version to emulate.",
        examples=["1.13.0"],
    )
    provisioning_sa_email: str | None = Field(
        default=None,
        description="Service account email to impersonate for Terraform provisioning.",
    )
    gcp_cluster_admin_sa: str | None = Field(
        default=None,
        description="Service account email granted cluster-admin access in the cluster.",
    )
    gce_network: str = Field(
        default="gem-clusters-vpc",
        description="VPC network name.",
    )
    gce_subnetwork: str = Field(
        default="gem-clusters-subnet",
        description="Subnetwork name.",
    )
    node_storage_size: str = Field(
        default="100GB",
        description="Size of node local storage partition before TopoLVM allocation.",
    )
    pod_cidr_blocks: str = Field(
        default="10.0.0.0/17",
        description="CIDR block for Kubernetes Pod network.",
    )
    services_cidr_blocks: str = Field(
        default="10.96.0.0/12",
        description="CIDR block for Kubernetes Service network.",
    )
    max_pods_per_node: int = Field(
        default=250,
        ge=1,
        le=250,
        description="Maximum pods schedulable per node.",
    )
    secondary_networks: list[SecondaryNetworkConfig] | None = Field(
        default=None,
        description="Optional list of secondary network definitions.",
    )

    @field_validator("cluster_name")
    @classmethod
    def validate_cluster_name(cls, v: str) -> str:
        v = v.strip()
        if not v:
            raise ValueError("cluster_name cannot be empty.")
        if len(v) > 26:
            raise ValueError(
                f"cluster_name '{v}' exceeds the maximum allowed length of 26 characters."
            )
        if not re.match(r"^[a-z][a-z0-9-]*[a-z0-9]$", v) and len(v) > 1:
            raise ValueError(
                f"cluster_name '{v}' must be lowercase alphanumeric with hyphens, starting with a letter."
            )
        return v

    @field_validator("pod_cidr_blocks", "services_cidr_blocks")
    @classmethod
    def validate_network_cidr(cls, v: str) -> str:
        v = v.strip()
        try:
            ipaddress.ip_network(v, strict=False)
        except ValueError:
            raise ValueError(f"'{v}' is not a valid IPv4 CIDR block.")
        return v

    @field_validator("hardware_variant")
    @classmethod
    def validate_hardware_variant(cls, v: str) -> str:
        valid = get_valid_hardware_variants()
        if v not in valid:
            raise ValueError(
                f"Invalid hardware_variant '{v}'. Allowed options: {', '.join(sorted(valid))}."
            )
        return v

    @field_validator("emulate_gdc_version")
    @classmethod
    def validate_gdc_version(cls, v: str) -> str:
        valid = get_valid_gdc_versions()
        if v not in valid:
            raise ValueError(
                f"Invalid emulate_gdc_version '{v}'. Allowed options: {', '.join(sorted(valid))}."
            )
        return v

    @field_validator(
        "project_id",
        "zone",
        "region",
        "provisioning_sa_email",
        "gcp_cluster_admin_sa",
        mode="before",
    )
    @classmethod
    def sanitize_optional_fields(cls, v: Any) -> Any:
        return sanitize_optional_str(v)

    @field_validator("provisioning_sa_email")
    @classmethod
    def validate_provisioning_sa(cls, v: str | None) -> str | None:
        return validate_email_or_sa(v, "provisioning_sa_email")

    @field_validator("gcp_cluster_admin_sa")
    @classmethod
    def validate_admin_sa(cls, v: str | None) -> str | None:
        return validate_email_or_sa(v, "gcp_cluster_admin_sa")

    @field_validator("project_id")
    @classmethod
    def validate_project(cls, v: str | None) -> str | None:
        return validate_project_id(v)

    @field_validator("node_storage_size")
    @classmethod
    def validate_node_storage(cls, v: str) -> str:
        return validate_storage_size(v)

    @model_validator(mode="after")
    def validate_cluster_parameters(self) -> "ClusterCreateRequest":
        # Validate zone and region syntax, consistency, and auto-derivation
        self.zone, self.region = validate_zone_and_region(self.zone, self.region)

        pid = self.project_id or "gem-default-project"
        z = self.zone or "us-central1-a"
        cname = self.cluster_name
        # Kubernetes node registration FQDN limit check: len(cluster_name) + len(zone) + len(project_id) + 15 <= 63
        if len(cname) + len(z) + len(pid) + 15 > 63:
            max_cname_len = max(1, 63 - len(z) - len(pid) - 15)
            raise ValueError(
                f"Cluster name '{cname}' is too long for project '{pid}' and zone '{z}'. "
                f"Combined node FQDNs would exceed the 63-character Kubernetes metadata limit. "
                f"Maximum allowed length for this project/zone is {max_cname_len} characters."
            )
        return self


class ClusterDeleteRequest(BaseModel):
    """Request payload for tearing down an existing GEM cluster."""

    cluster_name: str = Field(
        ...,
        description="Exact identifier of the cluster to tear down.",
        examples=["gem-cluster-1"],
    )
    project_id: str | None = Field(
        default=None,
        description="Target GCP Project ID hosting the cluster. Defaults to environment project.",
    )
    zone: str | None = Field(
        default=None,
        description="GCP Zone where cluster node VMs reside. Defaults to environment zone.",
    )
    region: str | None = Field(
        default=None,
        description="GCP Region. Defaults to derived zone region.",
    )
    provisioning_sa_email: str | None = Field(
        default=None,
        description="Email of the Terraform provisioning SA to impersonate.",
    )
    tf_state_bucket: str | None = Field(
        default=None,
        description="GCS bucket holding remote state (e.g., 'gem-<project_id>-tfstate').",
    )

    @field_validator("cluster_name")
    @classmethod
    def validate_cluster_name(cls, v: str) -> str:
        v = v.strip()
        if not v:
            raise ValueError("cluster_name is required for teardown.")
        if len(v) > 26:
            raise ValueError(
                f"cluster_name '{v}' exceeds the maximum allowed length of 26 characters."
            )
        if not re.match(r"^[a-z][a-z0-9-]*[a-z0-9]$", v) and len(v) > 1:
            raise ValueError(
                f"cluster_name '{v}' must be lowercase alphanumeric with hyphens, starting with a letter."
            )
        return v

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
    def validate_delete_parameters(self) -> "ClusterDeleteRequest":
        self.zone, self.region = validate_zone_and_region(self.zone, self.region)
        return self


class ClusterInfo(BaseModel):
    """Details of a deployed GEM cluster corresponding to gcloud container clusters list."""

    name: str = Field(..., description="Cluster identifier")
    location: str = Field(..., description="GCP region or zone location")
    master_version: str = Field(
        ..., description="Kubernetes / Anthos Bare Metal version"
    )
    status: Literal["RUNNING", "PROVISIONING", "DEGRADED", "STOPPED", "ERROR"] = Field(
        default="RUNNING", description="Current cluster operational state"
    )
    node_count: int = Field(default=3, description="Total node count in cluster")
    endpoint: str | None = Field(
        default=None, description="Cluster control plane endpoint or VIP"
    )
    create_time: str | None = Field(
        default=None, description="Cluster creation timestamp"
    )
    hardware_variant: str | None = Field(
        default=None, description="Hardware variant emulated"
    )
    gdc_version: str | None = Field(default=None, description="Emulated GDC version")
    emulate_gdc_version: str | None = Field(
        default=None, description="Emulated GDC version"
    )
    project_id: str | None = Field(default=None, description="GCP Project ID")


class ClusterListResponse(BaseModel):
    """Response containing list of deployed GEM clusters."""

    clusters: list[ClusterInfo] = Field(default_factory=list)
    total: int = 0
