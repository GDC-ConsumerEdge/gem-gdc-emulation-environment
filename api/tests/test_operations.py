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

import pytest
from fastapi.testclient import TestClient
from httpx2 import AsyncClient

from gem_api.models.operations import OperationStatus, OperationType
from gem_api.services.operations import get_operation_manager


def test_operation_lifecycle_and_status(client: TestClient):
    # Initiate an operation
    r = client.post(
        "/api/v1/clusters/create",
        json={"cluster_name": "gem-op-test", "project_id": "test-project-123"},
    )
    assert r.status_code == 202
    op_id = r.json()["operation_id"]

    # Query status
    status_resp = client.get(f"/api/v1/operations/{op_id}")
    assert status_resp.status_code == 200
    data = status_resp.json()
    assert data["operation_id"] == op_id
    assert data["target_resource"] == "gem-op-test"
    assert data["operation_type"] == "CLUSTER_CREATE"


def test_get_operation_not_found(client: TestClient):
    response = client.get("/api/v1/operations/non-existent-op")
    assert response.status_code == 404
    assert "not found" in response.json()["detail"]


def test_operation_logs_and_tail(client: TestClient):
    op_mgr = get_operation_manager()
    op_id = "test-log-op"
    asyncio.run(
        op_mgr.register_operation(
            operation_id=op_id,
            operation_type=OperationType.CLUSTER_CREATE,
            target_resource="test-log-op",
        )
    )
    for i in range(10):
        op_mgr.append_log(op_id, f"Log message line {i}")

    # Fetch all logs
    r_all = client.get(f"/api/v1/operations/{op_id}/logs")
    assert r_all.status_code == 200
    data = r_all.json()
    assert len(data["log_lines"]) >= 10

    # Fetch tailed logs
    r_tail = client.get(f"/api/v1/operations/{op_id}/logs?tail=3")
    assert r_tail.status_code == 200
    tail_data = r_tail.json()
    assert len(tail_data["log_lines"]) == 3
    assert "line 9" in tail_data["log_lines"][-1]


def test_cancel_operation(client: TestClient):
    # Register an active operation
    r = client.post(
        "/api/v1/clusters/create",
        json={"cluster_name": "gem-cancel-op", "project_id": "test-project-123"},
    )
    assert r.status_code == 202

    # Cancel operation
    cancel_resp = client.post("/api/v1/operations/gem-cancel-op/cancel")
    assert cancel_resp.status_code == 200
    data = cancel_resp.json()
    assert data["success"] is True
    assert data["status"] == "CANCELLED"
    assert "cancelled" in data["message"]

    # Cancelling again returns success=False
    cancel_again = client.post("/api/v1/operations/gem-cancel-op/cancel")
    assert cancel_again.status_code == 200
    assert cancel_again.json()["success"] is False


@pytest.mark.asyncio
async def test_operation_sse_log_streaming():
    op_mgr = get_operation_manager()
    op_id = "sse-test-op"
    await op_mgr.register_operation(
        operation_id=op_id,
        operation_type=OperationType.CLUSTER_CREATE,
        target_resource=op_id,
    )
    op_mgr.append_log(op_id, "SSE line 1")
    op_mgr.append_log(op_id, "SSE line 2")
    await op_mgr.update_operation(
        op_id, status=OperationStatus.SUCCEEDED, completed=True
    )

    streamed_lines = []
    async for line in op_mgr.stream_logs(op_id):
        streamed_lines.append(line)

    assert len(streamed_lines) >= 3
    assert any("SSE line 1" in line for line in streamed_lines)
    assert any("SSE line 2" in line for line in streamed_lines)


@pytest.mark.asyncio
async def test_operation_sse_endpoint(async_client: AsyncClient):
    op_mgr = get_operation_manager()
    op_id = "sse-http-op"
    await op_mgr.register_operation(
        operation_id=op_id,
        operation_type=OperationType.CLUSTER_CREATE,
        target_resource=op_id,
    )
    op_mgr.append_log(op_id, "HTTP SSE Line")
    await op_mgr.update_operation(
        op_id, status=OperationStatus.SUCCEEDED, completed=True
    )

    async with async_client.stream(
        "GET", f"/api/v1/operations/{op_id}/logs?stream=true"
    ) as response:
        assert response.status_code == 200
        assert "text/event-stream" in response.headers.get("content-type", "")
        lines = [line async for line in response.aiter_lines() if line]
        assert any("HTTP SSE Line" in line for line in lines)
