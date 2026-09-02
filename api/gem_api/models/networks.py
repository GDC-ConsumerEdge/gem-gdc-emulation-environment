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

from pydantic import BaseModel, Field


class SecondaryNetworkItem(BaseModel):
    """Secondary network status on a GEM cluster."""

    name: str = Field(..., description="Network name")
    vlan_id: int | str = Field(..., description="VLAN identifier")
    subnet: str = Field(..., description="Subnet CIDR")
    gateway: str | None = Field(default=None, description="Default gateway IP")
    vip_pool: str | None = Field(default=None, description="Allocated VIP range")
    purpose: str | None = Field(
        default="Secondary VLAN Overlay", description="Network purpose"
    )
    interface_name: str | None = Field(
        default=None, description="Host interface name (e.g. gdcenet0.123)"
    )
    status: str = Field(default="Active", description="Operational status")


class SecondaryNetworkListResponse(BaseModel):
    """List of secondary networks deployed on a cluster."""

    networks: list[SecondaryNetworkItem] = Field(default_factory=list)
