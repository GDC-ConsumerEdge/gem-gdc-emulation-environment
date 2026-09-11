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


class NodeStatusItem(BaseModel):
    """Cluster node status and utilization metrics."""

    name: str = Field(..., description="Node hostname")
    status: str = Field(default="Ready", description="Kubelet readiness condition")
    role: str = Field(
        default="Worker Node", description="Node role (Control Plane or Worker Node)"
    )
    ip: str = Field(default="0.0.0.0", description="Internal node IP address")
    cpu_usage: str = Field(default="320m", description="Current CPU usage")
    cpu_percent: int = Field(default=4, description="CPU usage percentage")
    mem_usage: str = Field(default="3100Mi", description="Current memory usage")
    mem_percent: int = Field(default=5, description="Memory usage percentage")


class ClusterMetrics(BaseModel):
    """Aggregated capacity and utilization metrics across the cluster."""

    total_cpu: str = Field(default="96 vCPU", description="Total cluster vCPU capacity")
    used_cpu: str = Field(default="18 vCPU", description="Total allocated/used vCPU")
    total_mem: str = Field(default="192 GB", description="Total cluster RAM capacity")
    used_mem: str = Field(default="42 GB", description="Total allocated/used RAM")
    storage_allocated: str = Field(
        default="850 GB / 3.9 TB", description="TopoLVM storage usage"
    )


class ClusterStatusResponse(BaseModel):
    """Cluster health, node inventory, and live resource utilization."""

    connected: bool = Field(
        ..., description="Whether cluster is reachable via GKE Connect Gateway"
    )
    cluster_name: str = Field(..., description="Cluster identifier")
    mode: str = Field(default="Live Connected", description="Connection mode")
    nodes: list[NodeStatusItem] = Field(default_factory=list)
    metrics: ClusterMetrics | None = Field(default=None)
