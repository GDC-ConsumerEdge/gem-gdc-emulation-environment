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


class ContainerStatusItem(BaseModel):
    """Container status details within a Pod."""

    name: str = Field(..., description="Container name")
    image: str = Field(..., description="Container image")
    ready: bool = Field(default=True, description="Whether container is ready")
    state: str = Field(
        default="running", description="State (e.g. running, terminated, waiting)"
    )


class PodItem(BaseModel):
    """Kubernetes Pod summary on a GEM cluster."""

    name: str = Field(..., description="Pod name")
    namespace: str = Field(default="default", description="Kubernetes namespace")
    status: str = Field(default="Running", description="Pod lifecycle phase / status")
    ready: str = Field(default="1/1", description="Ready containers count string")
    restarts: int = Field(default=0, description="Total container restart count")
    age: str | None = Field(default=None, description="Human readable pod age")
    ip: str | None = Field(default=None, description="Primary Pod IP address")
    node_name: str | None = Field(default=None, description="Node running this pod")
    containers: list[ContainerStatusItem] = Field(default_factory=list)


class PodListResponse(BaseModel):
    """List of pods running on the cluster."""

    pods: list[PodItem] = Field(default_factory=list)


class PodCreateRequest(BaseModel):
    """Request payload for deploying a Pod."""

    name: str = Field(..., description="Pod name", examples=["nginx-webserver"])
    namespace: str = Field(
        default="default", description="Kubernetes namespace", examples=["default"]
    )
    image: str = Field(
        ..., description="Container image to run", examples=["nginx:alpine"]
    )
    command: list[str] | None = Field(
        default=None, description="Container command / entrypoint"
    )
    port: int | None = Field(
        default=None, description="Container port to expose", examples=[80]
    )
    env: dict[str, str] | None = Field(
        default=None, description="Environment variable key-value pairs"
    )
    labels: dict[str, str] | None = Field(default=None, description="Pod labels")
    annotations: dict[str, str] | None = Field(
        default=None,
        description="Pod annotations (e.g., networking.gke.io/interfaces)",
    )
    raw_manifest: str | None = Field(
        default=None,
        description="Optional full YAML/JSON raw manifest string for advanced pod definitions",
    )


class PodActionResponse(BaseModel):
    """Response returned upon creating or deleting a pod."""

    success: bool = True
    pod_name: str
    namespace: str = "default"
    message: str
