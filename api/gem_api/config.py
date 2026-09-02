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
import os
import shutil
import subprocess
from functools import lru_cache
from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict

logger = logging.getLogger("gem_api.config")


def _resolve_default_project() -> str:
    pid = os.getenv("PROJECT_ID", os.getenv("GCP_PROJECT", "")).strip()
    if pid:
        return pid
    if shutil.which("gcloud"):
        try:
            res = subprocess.run(
                ["gcloud", "config", "get-value", "project"],
                capture_output=True,
                text=True,
                timeout=3.0,
                check=False,
            )
            if res.returncode == 0:
                val = res.stdout.strip()
                if val and val != "(unset)":
                    return val
        except (subprocess.SubprocessError, OSError, ValueError) as e:
            logger.debug("Could not resolve default project from gcloud: %s", e)
    return "gem-default-project"


def _resolve_default_zone() -> str:
    zone = os.getenv("GEM_GCP_ZONE", os.getenv("CLOUDSDK_COMPUTE_ZONE", "")).strip()
    if zone:
        return zone
    if shutil.which("gcloud"):
        try:
            res = subprocess.run(
                ["gcloud", "config", "get-value", "compute/zone"],
                capture_output=True,
                text=True,
                timeout=3.0,
                check=False,
            )
            if res.returncode == 0:
                val = res.stdout.strip()
                if val and val != "(unset)":
                    return val
        except (subprocess.SubprocessError, OSError, ValueError) as e:
            logger.debug("Could not resolve default zone from gcloud: %s", e)
    return "us-central1-a"


class Settings(BaseSettings):
    """Application settings and runtime environment configuration."""

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    app_name: str = "GEM REST API"
    app_version: str = "0.1.0"
    debug: bool = False

    # Server binding
    port: int = int(os.getenv("PORT", "8080"))
    host: str = os.getenv("HOST", "0.0.0.0")

    # Repository Root Directory (defaults to parent of 'api')
    repo_root: Path = Path(
        os.getenv("REPO_ROOT", Path(__file__).resolve().parent.parent.parent)
    )

    # GCP Environment Defaults
    default_project_id: str = _resolve_default_project()
    default_zone: str = _resolve_default_zone()

    @property
    def default_region(self) -> str:
        """Derive GCP region from the default zone (e.g. 'us-central1-a' -> 'us-central1')."""
        if "-" in self.default_zone:
            return "-".join(self.default_zone.split("-")[:-1])
        return "us-central1"

    # Log directories and buffering
    log_dir: Path = Path(os.getenv("GEM_LOG_DIR", "/tmp/gem-api/logs"))
    max_log_buffer_lines: int = 1000

    def get_provisioning_sa(self, project_id: str | None = None) -> str:
        """Get the default provisioning service account email for a project."""
        pid = project_id or self.default_project_id
        return f"tf-provisioner@{pid}.iam.gserviceaccount.com"

    def get_cluster_admin_sa(self, project_id: str | None = None) -> str:
        """Get the default cluster admin service account email for a project."""
        pid = project_id or self.default_project_id
        return f"gem-cluster-admin@{pid}.iam.gserviceaccount.com"

    def get_tf_state_bucket(self, project_id: str | None = None) -> str:
        """Get the default GCS state bucket name for a project."""
        pid = project_id or self.default_project_id
        return f"gem-{pid}-tfstate"


@lru_cache
def get_settings() -> Settings:
    """Return cached application settings singleton."""
    return Settings()
