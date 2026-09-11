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

from collections.abc import AsyncGenerator, Generator
from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from httpx2 import ASGITransport, AsyncClient

from gem_api.config import get_settings
from gem_api.main import app
from gem_api.services.operations import get_operation_manager


@pytest.fixture(autouse=True)
def setup_test_environment(monkeypatch: pytest.MonkeyPatch, tmp_path: Path):
    """Ensure mock execution is enabled and temporary log directory is configured for all tests."""
    monkeypatch.setenv("GEM_MOCK_RUNNER", "true")
    monkeypatch.setenv("GEM_LOG_DIR", str(tmp_path / "logs"))
    monkeypatch.setenv("PROJECT_ID", "test-project-123")
    monkeypatch.setenv("GEM_GCP_ZONE", "us-central1-a")

    get_settings.cache_clear()
    op_mgr = get_operation_manager()
    op_mgr.reset()

    yield

    op_mgr.reset()
    get_settings.cache_clear()


@pytest.fixture
def client() -> Generator[TestClient]:
    """Provide synchronous FastAPI TestClient."""
    with TestClient(app) as c:
        yield c


@pytest.fixture
async def async_client() -> AsyncGenerator[AsyncClient]:
    """Provide asynchronous httpx AsyncClient for async endpoint testing."""
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        yield ac
