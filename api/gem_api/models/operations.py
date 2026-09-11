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

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel, Field


class OperationStatus(StrEnum):
    """Execution status of an asynchronous operation."""

    QUEUED = "QUEUED"
    RUNNING = "RUNNING"
    SUCCEEDED = "SUCCEEDED"
    FAILED = "FAILED"
    CANCELLED = "CANCELLED"


class OperationType(StrEnum):
    """Categorical type of the executed background operation."""

    CLUSTER_CREATE = "CLUSTER_CREATE"
    CLUSTER_DELETE = "CLUSTER_DELETE"
    WORKSTATION_CREATE = "WORKSTATION_CREATE"
    WORKSTATION_DELETE = "WORKSTATION_DELETE"
    EDGE_ROUTER_CREATE = "EDGE_ROUTER_CREATE"
    EDGE_ROUTER_DELETE = "EDGE_ROUTER_DELETE"


class OperationAcceptedResponse(BaseModel):
    """Response returned with HTTP 202 Accepted upon initiating an async operation."""

    operation_id: str = Field(
        ..., description="Unique tracking identifier for this operation"
    )
    status: OperationStatus = Field(
        default=OperationStatus.QUEUED, description="Initial operation status"
    )
    message: str = Field(..., description="Succinct summary message")
    target_resource: str | None = Field(
        default=None, description="Target resource identifier"
    )


class OperationResponse(BaseModel):
    """Full operational status and progress breakdown for an async operation."""

    operation_id: str = Field(..., description="Unique operation identifier")
    operation_type: OperationType = Field(
        ..., description="Type of operation being executed"
    )
    status: OperationStatus = Field(..., description="Current execution state")
    target_resource: str = Field(
        ..., description="Resource target (e.g. cluster name or VM name)"
    )
    current_step: str | None = Field(
        default=None, description="Human-readable current execution step"
    )
    message: str | None = Field(
        default=None, description="Succinct status or progress update"
    )
    created_at: datetime | str = Field(
        ...,
        description="Timestamp when operation was initiated",
    )
    updated_at: datetime | str = Field(
        ...,
        description="Timestamp of last status update",
    )
    completed_at: datetime | str | None = Field(
        default=None,
        description="Timestamp when operation finalized",
    )
    error: str | None = Field(
        default=None,
        description="Error description if operation failed",
    )


class OperationLogsResponse(BaseModel):
    """Log lines associated with an operation."""

    operation_id: str = Field(..., description="Operation identifier")
    status: OperationStatus | None = Field(
        default=None, description="Current execution state"
    )
    log_lines: list[str] = Field(
        default_factory=list, description="Array of log output strings"
    )


class OperationCancelResponse(BaseModel):
    """Response returned when an operation cancellation request is processed."""

    success: bool = Field(
        ..., description="Whether the cancellation was successfully issued"
    )
    operation_id: str = Field(..., description="Operation identifier")
    status: OperationStatus = Field(
        default=OperationStatus.CANCELLED, description="New operation status"
    )
    message: str = Field(..., description="Summary message")
