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


class RootSyncItem(BaseModel):
    """GitOps Config Sync RootSync status."""

    name: str = Field(..., description="RootSync object name")
    namespace: str = Field(default="config-management-system", description="Namespace")
    repo: str = Field(..., description="Target Git repository URL")
    branch: str = Field(default="main", description="Git branch tracked")
    dir: str = Field(default="/", description="Directory path inside repository")
    auth: str = Field(
        default="none", description="Authentication mechanism (none, token, ssh)"
    )
    period: str = Field(default="15s", description="Sync interval period")
    status: str = Field(
        default="SYNCED", description="Synchronization status (SYNCED, PENDING, ERROR)"
    )
    commit: str = Field(..., description="Currently synced commit hash")
    last_synced: str = Field(..., description="Timestamp of latest sync reconciliation")
    message: str = Field(..., description="Sync status description or error message")


class RootSyncListResponse(BaseModel):
    """List of RootSyncs configured on the cluster."""

    root_syncs: list[RootSyncItem] = Field(default_factory=list)
