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

import asyncio
import logging

from fastapi import APIRouter, Depends, status

from gem_api.models.edge_router import (
    EdgeRouterCreateRequest,
    EdgeRouterDeleteRequest,
)
from gem_api.models.operations import (
    OperationAcceptedResponse,
    OperationType,
)
from gem_api.services.operations import OperationManager, get_operation_manager
from gem_api.services.runner import ProcessRunner, get_process_runner

logger = logging.getLogger("gem_api.routers.edge_router")

router = APIRouter(prefix="/edge-router", tags=["Edge Router"])


@router.post(
    "/create",
    response_model=OperationAcceptedResponse,
    status_code=status.HTTP_202_ACCEPTED,
    summary="Build a new GEM Edge Router",
    description=(
        "Initiates asynchronous provisioning of the GEM Edge Router VM via Terraform "
        "followed by Ansible configuration of Traefik reverse proxy and MetalLB VIP routing."
    ),
)
async def create_edge_router(
    request: EdgeRouterCreateRequest = EdgeRouterCreateRequest(),
    op_mgr: OperationManager = Depends(get_operation_manager),
    runner: ProcessRunner = Depends(get_process_runner),
) -> OperationAcceptedResponse:
    """Build the GEM Edge Router asynchronously."""
    operation_id = request.edge_router_name
    target_resource = request.edge_router_name

    record = await op_mgr.register_operation(
        operation_id=operation_id,
        operation_type=OperationType.EDGE_ROUTER_CREATE,
        target_resource=target_resource,
        initial_step="Terraform Provisioning (1/2)",
        initial_message=f"Starting build for edge router '{target_resource}'...",
    )

    task = asyncio.create_task(runner.run_edge_router_create(request, operation_id))
    record.task = task

    return OperationAcceptedResponse(
        operation_id=operation_id,
        status=record.status,
        message=f"Edge router build initiated for '{target_resource}'.",
        target_resource=target_resource,
    )


@router.post(
    "/delete",
    response_model=OperationAcceptedResponse,
    status_code=status.HTTP_202_ACCEPTED,
    summary="Tear down the GEM Edge Router",
    description="Initiates asynchronous teardown of the GEM Edge Router VM via Terraform.",
)
async def delete_edge_router(
    request: EdgeRouterDeleteRequest = EdgeRouterDeleteRequest(),
    op_mgr: OperationManager = Depends(get_operation_manager),
    runner: ProcessRunner = Depends(get_process_runner),
) -> OperationAcceptedResponse:
    """Tear down the GEM Edge Router asynchronously."""
    operation_id = request.edge_router_name
    target_resource = request.edge_router_name

    record = await op_mgr.register_operation(
        operation_id=operation_id,
        operation_type=OperationType.EDGE_ROUTER_DELETE,
        target_resource=target_resource,
        initial_step="Terraform Destruction (1/1)",
        initial_message=f"Starting teardown for edge router '{target_resource}'...",
    )

    task = asyncio.create_task(runner.run_edge_router_delete(request, operation_id))
    record.task = task

    return OperationAcceptedResponse(
        operation_id=operation_id,
        status=record.status,
        message=f"Edge router teardown initiated for '{target_resource}'.",
        target_resource=target_resource,
    )
