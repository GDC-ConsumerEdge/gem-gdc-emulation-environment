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
import json
import logging
import shutil

from gem_api.config import get_settings
from gem_api.manifest import get_default_gdc_version, get_default_hardware_variant
from gem_api.models.clusters import ClusterInfo, ClusterListResponse
from gem_api.models.projects import ProjectItem, ProjectListResponse

logger = logging.getLogger("gem_api.gcp")


class GcpService:
    """Service for interacting with GCP resources, project listings, and fleet cluster discovery."""

    async def list_projects(self, limit: int = 50) -> ProjectListResponse:
        """List accessible GCP projects via gcloud CLI if available."""
        if shutil.which("gcloud"):
            try:
                proc = await asyncio.create_subprocess_exec(
                    "gcloud",
                    "projects",
                    "list",
                    f"--limit={limit}",
                    "--format=json",
                    stdout=asyncio.subprocess.PIPE,
                    stderr=asyncio.subprocess.PIPE,
                )
                stdout, _ = await asyncio.wait_for(proc.communicate(), timeout=5.0)
                if proc.returncode == 0 and stdout:
                    data = json.loads(stdout.decode("utf-8"))
                    projects = [
                        ProjectItem(
                            project_id=p.get("projectId", ""),
                            name=p.get("name", p.get("projectId", "")),
                        )
                        for p in data
                        if p.get("projectId")
                    ]
                    return ProjectListResponse(projects=projects)
            except (
                TimeoutError,
                OSError,
                ValueError,
                json.JSONDecodeError,
                KeyError,
            ) as e:
                logger.debug("gcloud projects list failed or timed out: %s", e)

        return ProjectListResponse(projects=[])

    async def list_clusters(self, project_id: str | None = None) -> ClusterListResponse:
        """List deployed GEM clusters in the specified GCP project."""
        settings = get_settings()
        pid = project_id or settings.default_project_id
        zone = settings.default_zone
        default_hw = get_default_hardware_variant()
        default_gdc = get_default_gdc_version()

        clusters_map: dict[str, ClusterInfo] = {}

        if shutil.which("gcloud"):
            # Discover via GKE Fleet Memberships
            try:
                proc = await asyncio.create_subprocess_exec(
                    "gcloud",
                    "container",
                    "fleet",
                    "memberships",
                    "list",
                    f"--project={pid}",
                    "--format=json",
                    stdout=asyncio.subprocess.PIPE,
                    stderr=asyncio.subprocess.PIPE,
                )
                stdout, _ = await asyncio.wait_for(proc.communicate(), timeout=8.0)
                if proc.returncode == 0 and stdout:
                    data = json.loads(stdout.decode("utf-8"))
                    for m in data:
                        raw_name = m.get("name", "").split("/")[-1]
                        if not raw_name:
                            continue
                        loc = m.get("monitoringConfig", {}).get("location") or zone
                        k8s_meta = m.get("endpoint", {}).get("kubernetesMetadata", {})
                        version = k8s_meta.get(
                            "kubernetesApiServerVersion", "1.34.100-gke.97"
                        )
                        node_count = k8s_meta.get("nodeCount", 3)
                        state_code = m.get("state", {}).get("code", "READY")
                        status = (
                            "RUNNING" if state_code in ("READY", "OK") else state_code
                        )

                        clusters_map[raw_name] = ClusterInfo(
                            name=raw_name,
                            location=loc,
                            master_version=version,
                            emulate_gdc_version=default_gdc,
                            status=status,
                            node_count=node_count,
                            endpoint=None,
                            hardware_variant=default_hw,
                            project_id=pid,
                            create_time=m.get("createTime"),
                        )
            except (
                TimeoutError,
                OSError,
                ValueError,
                json.JSONDecodeError,
                KeyError,
            ) as e:
                logger.debug("gcloud fleet memberships list failed: %s", e)

            # Discover via GCS Terraform State bucket (gs://gem-${pid}-tfstate/clusters/*)
            try:
                state_bucket = settings.get_tf_state_bucket(pid)
                proc = await asyncio.create_subprocess_exec(
                    "gcloud",
                    "storage",
                    "ls",
                    f"{state_bucket}/clusters/",
                    stdout=asyncio.subprocess.PIPE,
                    stderr=asyncio.subprocess.PIPE,
                )
                stdout, _ = await asyncio.wait_for(proc.communicate(), timeout=5.0)
                if proc.returncode == 0 and stdout:
                    for line in stdout.decode("utf-8").splitlines():
                        line = line.strip().rstrip("/")
                        if line:
                            cluster_name = line.split("/")[-1]
                            if cluster_name and cluster_name not in clusters_map:
                                clusters_map[cluster_name] = ClusterInfo(
                                    name=cluster_name,
                                    location=zone,
                                    master_version="1.34.100-gke.97",
                                    emulate_gdc_version=default_gdc,
                                    status="RUNNING",
                                    node_count=3,
                                    endpoint=None,
                                    hardware_variant=default_hw,
                                    project_id=pid,
                                )
            except (TimeoutError, OSError, ValueError, KeyError) as e:
                logger.debug("gcloud storage ls for state bucket failed: %s", e)

        cluster_list = list(clusters_map.values())
        return ClusterListResponse(clusters=cluster_list, total=len(cluster_list))


_gcp_service = GcpService()


def get_gcp_service() -> GcpService:
    """Return singleton GcpService instance."""
    return _gcp_service
