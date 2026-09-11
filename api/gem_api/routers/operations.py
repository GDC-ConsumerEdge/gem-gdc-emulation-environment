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

import logging

from fastapi import APIRouter, Depends, HTTPException, Query, status
from sse_starlette.sse import EventSourceResponse

from gem_api.models.operations import (
    OperationCancelResponse,
    OperationLogsResponse,
    OperationResponse,
)
from gem_api.services.operations import OperationManager, get_operation_manager

logger = logging.getLogger("gem_api.routers.operations")

router = APIRouter(prefix="/operations", tags=["Operations"])


@router.get(
    "/{operation_id}",
    response_model=OperationResponse,
    status_code=status.HTTP_200_OK,
    summary="Query operation status and progress",
    description="Returns the execution status, current step, and human-readable progress message for an operation.",
)
async def get_operation(
    operation_id: str,
    op_mgr: OperationManager = Depends(get_operation_manager),
) -> OperationResponse:
    """Retrieve details for a specific operation."""
    op = op_mgr.get_operation(operation_id)
    if not op:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail=f"Operation '{operation_id}' not found.",
        )
    return op


@router.get(
    "/{operation_id}/logs",
    summary="Query operation logs or stream live via Server-Sent Events",
    description="Returns either a snapshot of recent logs (JSON) or a live SSE event stream (text/event-stream).",
)
async def get_operation_logs(
    operation_id: str,
    tail: int | None = Query(
        default=None,
        ge=1,
        description="Number of latest log lines to return (JSON mode).",
    ),
    stream: bool = Query(
        default=False,
        description="Stream live logs in real-time using Server-Sent Events (SSE).",
    ),
    op_mgr: OperationManager = Depends(get_operation_manager),
):
    """Retrieve or stream logs for an operation."""
    if stream:

        async def event_generator():
            async for line in op_mgr.stream_logs(operation_id):
                yield {"data": line}

        return EventSourceResponse(event_generator())

    op = op_mgr.get_operation(operation_id)
    log_lines = op_mgr.get_logs(operation_id, tail=tail)

    return OperationLogsResponse(
        operation_id=operation_id,
        status=op.status if op else None,
        log_lines=log_lines,
    )


@router.post(
    "/{operation_id}/cancel",
    response_model=OperationCancelResponse,
    status_code=status.HTTP_200_OK,
    summary="Cancel / abort an in-progress operation",
    description="Terminates underlying subprocesses (SIGTERM/SIGKILL) and updates the operation status to CANCELLED.",
)
async def cancel_operation(
    operation_id: str,
    op_mgr: OperationManager = Depends(get_operation_manager),
) -> OperationCancelResponse:
    """Cancel an active operation."""
    return await op_mgr.cancel_operation(operation_id)
