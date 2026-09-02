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

from typing import Literal

from pydantic import BaseModel, Field


class VirtualMachineItem(BaseModel):
    """Details of a KubeVirt / VMRuntime virtual machine."""

    name: str = Field(..., description="Virtual Machine name")
    namespace: str = Field(default="default", description="Kubernetes namespace")
    status: str = Field(
        default="Running", description="VM status (e.g. Running, Stopped)"
    )
    cpus: int = Field(default=2, description="Allocated vCPU cores")
    memory: str = Field(default="4Gi", description="Allocated RAM (e.g. 4Gi, 8Gi)")
    ip: str | None = Field(default=None, description="Assigned guest IP address")
    image: str = Field(..., description="Base disk image identifier or URL")
    uptime: str | None = Field(default=None, description="VM runtime duration")
    power_state: str = Field(
        default="Running", description="Current power state (Running, Stopped)"
    )


class VirtualMachineListResponse(BaseModel):
    """List of virtual machines running on the cluster."""

    vms: list[VirtualMachineItem] = Field(default_factory=list)


class VirtualMachineDeployRequest(BaseModel):
    """Request payload for deploying a new Virtual Machine."""

    name: str = Field(
        ..., description="Virtual Machine name", examples=["ubuntu-edge-server-01"]
    )
    namespace: str = Field(
        default="default", description="Kubernetes namespace", examples=["default"]
    )
    cpus: int = Field(
        default=2, ge=1, le=128, description="Number of vCPU cores", examples=[4]
    )
    memory: str = Field(
        default="4Gi", description="RAM size (e.g. '4Gi', '8Gi')", examples=["8Gi"]
    )
    image: str = Field(
        ...,
        description="Preset image name or custom image URL",
        examples=["ubuntu-22.04-server-cloudimg-amd64"],
    )
    image_type: Literal["preset", "custom-url"] = Field(
        default="preset",
        description="Whether image is a known containerDisk preset or a custom HTTP URL",
    )


class VirtualMachinePowerRequest(BaseModel):
    """Request payload for starting or stopping a Virtual Machine."""

    namespace: str = Field(default="default", description="Kubernetes namespace")
    running: bool = Field(
        ..., description="Desired power state: true to start, false to stop"
    )


class VirtualMachinePowerResponse(BaseModel):
    """Response returned when a VM power state change is applied."""

    success: bool = Field(..., description="Whether the power command succeeded")
    vm_name: str = Field(..., description="Name of the affected virtual machine")
    power_state: str = Field(..., description="New power state of the VM")
    message: str = Field(..., description="Summary message")


class GenericActionResponse(BaseModel):
    """Generic action confirmation response."""

    success: bool = True
    message: str
