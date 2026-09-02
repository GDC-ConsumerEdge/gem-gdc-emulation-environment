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

from fastapi import APIRouter, Depends, Query, status

from gem_api.models.clusters import (
    ClusterCreateRequest,
    ClusterDeleteRequest,
    ClusterListResponse,
)
from gem_api.models.operations import (
    OperationAcceptedResponse,
    OperationType,
)
from gem_api.services.gcp_client import GcpService, get_gcp_service
from gem_api.services.operations import OperationManager, get_operation_manager
from gem_api.services.runner import ProcessRunner, get_process_runner

logger = logging.getLogger("gem_api.routers.clusters")

router = APIRouter(prefix="/clusters", tags=["Clusters"])


@router.post(
    "/create",
    response_model=OperationAcceptedResponse,
    status_code=status.HTTP_202_ACCEPTED,
    summary="Build a new GEM cluster",
    description=(
        "Initiates asynchronous provisioning of a 3-node GEM cluster via Terraform "
        "followed by Ansible cluster configuration (VXLAN, TopoLVM, GDC/ABM deployment)."
    ),
)
async def create_cluster(
    request: ClusterCreateRequest,
    op_mgr: OperationManager = Depends(get_operation_manager),
    runner: ProcessRunner = Depends(get_process_runner),
) -> OperationAcceptedResponse:
    """Build a new GEM cluster asynchronously."""
    operation_id = request.cluster_name
    record = await op_mgr.register_operation(
        operation_id=operation_id,
        operation_type=OperationType.CLUSTER_CREATE,
        target_resource=request.cluster_name,
        initial_step="Terraform Provisioning (1/2)",
        initial_message=f"Starting build for cluster '{request.cluster_name}'...",
    )

    task = asyncio.create_task(runner.run_cluster_create(request, operation_id))
    record.task = task

    return OperationAcceptedResponse(
        operation_id=operation_id,
        status=record.status,
        message=f"Cluster build initiated for '{request.cluster_name}'.",
        target_resource=request.cluster_name,
    )


@router.post(
    "/delete",
    response_model=OperationAcceptedResponse,
    status_code=status.HTTP_202_ACCEPTED,
    summary="Tear down a GEM cluster",
    description=(
        "Initiates asynchronous teardown of a GEM cluster via Ansible cleanup "
        "followed by Terraform destruction of compute and network resources."
    ),
)
async def delete_cluster(
    request: ClusterDeleteRequest,
    op_mgr: OperationManager = Depends(get_operation_manager),
    runner: ProcessRunner = Depends(get_process_runner),
) -> OperationAcceptedResponse:
    """Tear down a GEM cluster asynchronously."""
    operation_id = request.cluster_name
    record = await op_mgr.register_operation(
        operation_id=operation_id,
        operation_type=OperationType.CLUSTER_DELETE,
        target_resource=request.cluster_name,
        initial_step="Ansible Cleanup (1/2)",
        initial_message=f"Starting teardown for cluster '{request.cluster_name}'...",
    )

    task = asyncio.create_task(runner.run_cluster_delete(request, operation_id))
    record.task = task

    return OperationAcceptedResponse(
        operation_id=operation_id,
        status=record.status,
        message=f"Cluster teardown initiated for '{request.cluster_name}'.",
        target_resource=request.cluster_name,
    )


@router.get(
    "",
    response_model=ClusterListResponse,
    status_code=status.HTTP_200_OK,
    summary="List all deployed GEM clusters",
    description="Returns all deployed GEM clusters and their fleet/status attributes.",
)
async def list_clusters(
    project_id: str | None = Query(
        default=None,
        description="Filter by GCP Project ID. Defaults to active environment project.",
    ),
    gcp_service: GcpService = Depends(get_gcp_service),
) -> ClusterListResponse:
    """List all deployed GEM clusters."""
    return await gcp_service.list_clusters(project_id=project_id)
