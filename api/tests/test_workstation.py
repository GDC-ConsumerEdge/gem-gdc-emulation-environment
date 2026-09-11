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


def test_create_workstation_success(client: TestClient):
    payload = {
        "project_id": "test-project-123",
        "zone": "us-central1-a",
    }
    response = client.post("/api/v1/workstation/create", json=payload)
    assert response.status_code == 202
    data = response.json()
    assert data["operation_id"] == "gem-admin-ws"
    assert data["target_resource"] == "gem-admin-ws"


def test_create_workstation_conflict_duplicate(client: TestClient):
    payload = {"project_id": "test-project-123"}
    r1 = client.post("/api/v1/workstation/create", json=payload)
    assert r1.status_code == 202

    r2 = client.post("/api/v1/workstation/create", json=payload)
    assert r2.status_code == 409
    assert "already in progress" in r2.json()["detail"]


def test_delete_workstation_success(client: TestClient):
    payload = {"project_id": "test-project-123"}
    response = client.post("/api/v1/workstation/delete", json=payload)
    assert response.status_code == 202
    data = response.json()
    assert data["operation_id"] == "gem-admin-ws"
    assert data["target_resource"] == "gem-admin-ws"
