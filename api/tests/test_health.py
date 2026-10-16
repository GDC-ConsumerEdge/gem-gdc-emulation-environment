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

from gem_api import __version__


def test_healthz_endpoint(client: TestClient):
    response = client.get("/healthz")
    assert response.status_code == 200
    data = response.json()
    assert data["status"] == "ok"
    assert "GEM REST API" in data["app"]
    assert data["version"] == __version__


def test_health_endpoint(client: TestClient):
    response = client.get("/health")
    assert response.status_code == 200
    data = response.json()
    assert data["status"] == "ok"
    assert "GEM REST API" in data["app"]
    assert data["version"] == __version__


def test_api_v1_health_endpoint(client: TestClient):
    response = client.get("/api/v1/health")
    assert response.status_code == 200
    data = response.json()
    assert data["status"] == "ok"
    assert "GEM REST API" in data["app"]
    assert data["version"] == __version__


def test_openapi_version_matches_package_version(client: TestClient):
    """The OpenAPI document reports the package version.

    gem_api.__init__ is the only place the version is declared, and it is
    rewritten by release-please. This fails if someone reintroduces a literal.
    """
    response = client.get("/openapi.json")
    assert response.status_code == 200
    assert response.json()["info"]["version"] == __version__


def test_run_cli_forwards_uvicorn_args_and_settings(monkeypatch):
    """run_cli seeds UVICORN_HOST/PORT from Settings and forwards CLI flags."""
    import os
    from unittest.mock import MagicMock

    import uvicorn.main

    from gem_api.config import get_settings
    from gem_api.main import run_cli

    monkeypatch.delenv("UVICORN_HOST", raising=False)
    monkeypatch.delenv("UVICORN_PORT", raising=False)
    monkeypatch.delenv("UVICORN_RELOAD", raising=False)
    monkeypatch.setenv("HOST", "127.0.0.1")
    monkeypatch.setenv("PORT", "9090")
    monkeypatch.setenv("DEBUG", "true")
    get_settings.cache_clear()

    mock_uvicorn_main = MagicMock()
    monkeypatch.setattr(uvicorn.main, "main", mock_uvicorn_main)

    try:
        run_cli(["--workers", "2", "--log-level", "debug"])
        assert os.environ["UVICORN_HOST"] == "127.0.0.1"
        assert os.environ["UVICORN_PORT"] == "9090"
        assert os.environ["UVICORN_RELOAD"] == "true"
        mock_uvicorn_main.assert_called_once_with(
            args=["gem_api.main:app", "--workers", "2", "--log-level", "debug"],
            auto_envvar_prefix="UVICORN",
        )
    finally:
        get_settings.cache_clear()
