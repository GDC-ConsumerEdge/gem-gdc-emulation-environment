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

from gem_api.models.operations import (
    OperationAcceptedResponse,
    OperationType,
)
from gem_api.models.workstation import (
    WorkstationCreateRequest,
    WorkstationDeleteRequest,
)
from gem_api.services.operations import OperationManager, get_operation_manager
from gem_api.services.runner import ProcessRunner, get_process_runner

logger = logging.getLogger("gem_api.routers.workstation")

router = APIRouter(prefix="/workstation", tags=["Admin Workstation"])


@router.post(
    "/create",
    response_model=OperationAcceptedResponse,
    status_code=status.HTTP_202_ACCEPTED,
    summary="Build a new GEM Admin Workstation",
    description=(
        "Initiates asynchronous provisioning of the shared GEM Admin Workstation VM via Terraform "
        "followed by Ansible configuration of bmctl, docker, and cluster management tools."
    ),
)
async def create_workstation(
    request: WorkstationCreateRequest = WorkstationCreateRequest(),
    op_mgr: OperationManager = Depends(get_operation_manager),
    runner: ProcessRunner = Depends(get_process_runner),
) -> OperationAcceptedResponse:
    """Build the GEM Admin Workstation asynchronously."""
    operation_id = "gem-admin-ws"
    target_resource = "gem-admin-ws"

    record = await op_mgr.register_operation(
        operation_id=operation_id,
        operation_type=OperationType.WORKSTATION_CREATE,
        target_resource=target_resource,
        initial_step="Terraform Provisioning (1/2)",
        initial_message="Starting Admin Workstation build...",
    )

    task = asyncio.create_task(runner.run_workstation_create(request, operation_id))
    record.task = task

    return OperationAcceptedResponse(
        operation_id=operation_id,
        status=record.status,
        message="Admin Workstation build initiated.",
        target_resource=target_resource,
    )


@router.post(
    "/delete",
    response_model=OperationAcceptedResponse,
    status_code=status.HTTP_202_ACCEPTED,
    summary="Tear down the GEM Admin Workstation",
    description="Initiates asynchronous teardown of the GEM Admin Workstation VM via Terraform.",
)
async def delete_workstation(
    request: WorkstationDeleteRequest = WorkstationDeleteRequest(),
    op_mgr: OperationManager = Depends(get_operation_manager),
    runner: ProcessRunner = Depends(get_process_runner),
) -> OperationAcceptedResponse:
    """Tear down the GEM Admin Workstation asynchronously."""
    operation_id = "gem-admin-ws"
    target_resource = "gem-admin-ws"

    record = await op_mgr.register_operation(
        operation_id=operation_id,
        operation_type=OperationType.WORKSTATION_DELETE,
        target_resource=target_resource,
        initial_step="Terraform Destruction (1/1)",
        initial_message="Starting Admin Workstation teardown...",
    )

    task = asyncio.create_task(runner.run_workstation_delete(request, operation_id))
    record.task = task

    return OperationAcceptedResponse(
        operation_id=operation_id,
        status=record.status,
        message="Admin Workstation teardown initiated.",
        target_resource=target_resource,
    )
