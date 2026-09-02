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

from fastapi.testclient import TestClient

from gem_api.models.projects import ProjectItem, ProjectListResponse
from gem_api.services.gcp_client import get_gcp_service


def test_list_projects_empty_when_no_gcp_projects(client: TestClient):
    response = client.get("/api/v1/projects")
    assert response.status_code == 200
    data = response.json()
    assert "projects" in data
    assert isinstance(data["projects"], list)


def test_list_projects_with_results(client: TestClient):
    gcp_service = get_gcp_service()

    async def mock_list_projects(limit: int = 50) -> ProjectListResponse:
        _ = limit
        return ProjectListResponse(
            projects=[
                ProjectItem(project_id="test-project-1", name="Test Project 1"),
                ProjectItem(project_id="test-project-2", name="Test Project 2"),
            ]
        )

    orig_method = gcp_service.list_projects
    gcp_service.list_projects = mock_list_projects
    try:
        response = client.get("/api/v1/projects?limit=10")
        assert response.status_code == 200
        data = response.json()
        assert len(data["projects"]) == 2
        assert data["projects"][0]["project_id"] == "test-project-1"
    finally:
        gcp_service.list_projects = orig_method
