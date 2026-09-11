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

from fastapi import APIRouter, Depends, Query, status

from gem_api.models.projects import ProjectListResponse
from gem_api.services.gcp_client import GcpService, get_gcp_service

logger = logging.getLogger("gem_api.routers.projects")

router = APIRouter(prefix="/projects", tags=["Projects"])


@router.get(
    "",
    response_model=ProjectListResponse,
    status_code=status.HTTP_200_OK,
    summary="List accessible GCP Projects",
    description="Returns available GCP projects for cluster deployment.",
)
async def list_projects(
    limit: int = Query(
        default=50,
        ge=1,
        le=500,
        description="Maximum number of projects to return.",
    ),
    gcp_service: GcpService = Depends(get_gcp_service),
) -> ProjectListResponse:
    """List accessible GCP projects."""
    return await gcp_service.list_projects(limit=limit)
