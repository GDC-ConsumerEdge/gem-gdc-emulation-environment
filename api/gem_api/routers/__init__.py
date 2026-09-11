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

from gem_api.routers.cluster_operations import router as cluster_operations_router
from gem_api.routers.clusters import router as clusters_router
from gem_api.routers.edge_router import router as edge_router_router
from gem_api.routers.operations import router as operations_router
from gem_api.routers.projects import router as projects_router
from gem_api.routers.workstation import router as workstation_router

__all__ = [
    "cluster_operations_router",
    "clusters_router",
    "edge_router_router",
    "operations_router",
    "projects_router",
    "workstation_router",
]
