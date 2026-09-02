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
from collections.abc import AsyncGenerator
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from gem_api.config import get_settings
from gem_api.routers import (
    cluster_operations_router,
    clusters_router,
    edge_router_router,
    operations_router,
    projects_router,
    workstation_router,
)

logger = logging.getLogger("gem_api")
logging.basicConfig(
    level=logging.INFO,
    format="[%(asctime)s] [%(levelname)s] [%(name)s]: %(message)s",
)


@asynccontextmanager
async def lifespan(_app: FastAPI) -> AsyncGenerator[None]:
    """Lifespan context manager for startup preparation and graceful shutdown."""
    settings = get_settings()
    settings.log_dir.mkdir(parents=True, exist_ok=True)
    logger.info(
        "Starting %s v%s (Host: %s, Port: %d)",
        settings.app_name,
        settings.app_version,
        settings.host,
        settings.port,
    )
    logger.info("Log directory initialized at %s", settings.log_dir)

    yield

    logger.info("Shutting down %s...", settings.app_name)


app = FastAPI(
    title="GEM REST API",
    description="REST API for the Google Distributed Cloud (GDC) Connected Emulation Environment (GEM).",
    version="0.1.0",
    docs_url="/docs",
    redoc_url="/redoc",
    openapi_url="/openapi.json",
    lifespan=lifespan,
)

# Enable CORS for frontend clients
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Include API Routers under /api/v1
app.include_router(clusters_router, prefix="/api/v1")
app.include_router(workstation_router, prefix="/api/v1")
app.include_router(edge_router_router, prefix="/api/v1")
app.include_router(operations_router, prefix="/api/v1")
app.include_router(projects_router, prefix="/api/v1")
app.include_router(cluster_operations_router, prefix="/api/v1")


@app.get(
    "/healthz",
    tags=["Health"],
    summary="Health check",
    description="Returns service health status for Kubernetes / Cloud Run probes.",
)
async def healthz():
    """Liveness/Readiness probe endpoint."""
    settings = get_settings()
    return {
        "status": "ok",
        "app": settings.app_name,
        "version": settings.app_version,
    }


@app.get(
    "/api/v1/health",
    tags=["Health"],
    summary="API v1 Health check",
    description="Returns API v1 health status.",
)
async def api_health():
    """API v1 health status endpoint."""
    settings = get_settings()
    return {
        "status": "ok",
        "app": settings.app_name,
        "version": settings.app_version,
    }


if __name__ == "__main__":
    settings = get_settings()
    uvicorn.run(
        "gem_api.main:app",
        host=settings.host,
        port=settings.port,
        reload=settings.debug,
    )
