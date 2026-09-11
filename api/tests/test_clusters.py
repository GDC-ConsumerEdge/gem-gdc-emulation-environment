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


def test_create_cluster_default_success(client: TestClient):
    payload = {
        "cluster_name": "gem-test-c1",
        "project_id": "test-project-123",
        "zone": "us-central1-a",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 202
    data = response.json()
    assert data["operation_id"] == "gem-test-c1"
    assert data["target_resource"] == "gem-test-c1"
    assert data["status"] in ("QUEUED", "RUNNING")


def test_create_cluster_conflict_when_already_running(client: TestClient):
    payload = {
        "cluster_name": "gem-test-c1",
        "project_id": "test-project-123",
        "zone": "us-central1-a",
    }
    # First request succeeds
    r1 = client.post("/api/v1/clusters/create", json=payload)
    assert r1.status_code == 202

    # Second concurrent request with same cluster name returns 409 Conflict
    r2 = client.post("/api/v1/clusters/create", json=payload)
    assert r2.status_code == 409
    err = r2.json()
    assert "already in progress" in err["detail"]


def test_create_cluster_name_length_validation(client: TestClient):
    # Cluster name > 26 chars
    payload = {
        "cluster_name": "this-cluster-name-is-way-too-long-for-gem",
        "project_id": "test-project-123",
        "zone": "us-central1-a",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 422


def test_create_cluster_invalid_cidr_validation(client: TestClient):
    payload = {
        "cluster_name": "gem-test-bad-cidr",
        "pod_cidr_blocks": "999.999.999.999/99",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 422


def test_create_cluster_invalid_hardware_variant_validation(client: TestClient):
    payload = {
        "cluster_name": "gem-test-bad-hw",
        "hardware_variant": "non-existent-hw-tier",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 422
    assert "Invalid hardware_variant" in response.text


def test_create_cluster_fqdn_length_validation(client: TestClient):
    # Cluster name + zone + project_id > 63 - 15 = 48 chars
    payload = {
        "cluster_name": "a-cluster-name-16",
        "project_id": "a-very-long-gcp-project-identifier-that-exceeds-bounds",
        "zone": "us-central1-a",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 422
    assert "63-character" in response.text


def test_delete_cluster_success(client: TestClient):
    payload = {
        "cluster_name": "gem-test-del",
        "project_id": "test-project-123",
    }
    response = client.post("/api/v1/clusters/delete", json=payload)
    assert response.status_code == 202
    data = response.json()
    assert data["operation_id"] == "gem-test-del"
    assert data["target_resource"] == "gem-test-del"


def test_delete_cluster_missing_name_fails(client: TestClient):
    payload = {
        "project_id": "test-project-123",
    }
    response = client.post("/api/v1/clusters/delete", json=payload)
    assert response.status_code == 422


def test_delete_cluster_invalid_name_type_fails(client: TestClient):
    # Pass an integer instead of string for cluster_name
    payload = {
        "cluster_name": 12345,
        "project_id": "test-project-123",
    }
    response = client.post("/api/v1/clusters/delete", json=payload)
    assert response.status_code == 422


def test_delete_cluster_empty_name_fails(client: TestClient):
    payload = {
        "cluster_name": "   ",
        "project_id": "test-project-123",
    }
    response = client.post("/api/v1/clusters/delete", json=payload)
    assert response.status_code == 422


def test_list_clusters_empty(client: TestClient):
    response = client.get("/api/v1/clusters?project_id=test-project-123")
    assert response.status_code == 200
    data = response.json()
    assert "clusters" in data
    assert isinstance(data["clusters"], list)


def test_list_clusters_with_results(client: TestClient):
    from gem_api.models.clusters import ClusterInfo, ClusterListResponse
    from gem_api.services.gcp_client import get_gcp_service

    gcp_service = get_gcp_service()

    async def mock_list_clusters(project_id: str | None = None) -> ClusterListResponse:
        _ = project_id
        return ClusterListResponse(
            clusters=[
                ClusterInfo(
                    name="gem-cluster-1",
                    location="us-central1-a",
                    master_version="1.34.100-gke.97",
                    emulate_gdc_version="1.13.0",
                    status="RUNNING",
                    node_count=3,
                    endpoint="10.200.54.2",
                    hardware_variant="g2-small-64gb",
                    project_id="test-project-123",
                )
            ]
        )

    orig_method = gcp_service.list_clusters
    gcp_service.list_clusters = mock_list_clusters
    try:
        response = client.get("/api/v1/clusters?project_id=test-project-123")
        assert response.status_code == 200
        data = response.json()
        assert len(data["clusters"]) == 1
        assert data["clusters"][0]["name"] == "gem-cluster-1"
        assert data["clusters"][0]["status"] == "RUNNING"
    finally:
        gcp_service.list_clusters = orig_method


def test_create_cluster_swapped_zone_and_region_fails_fast(client: TestClient):
    """User submitted zone='us-east5' and region='us-east5-b'. Fast-fail with 422 and helpful message."""
    payload = {
        "cluster_name": "api-cluster-swap",
        "project_id": "smooth-graph-498313-p2",
        "zone": "us-east5",
        "region": "us-east5-b",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 422
    assert "zone and region are swapped" in response.text
    assert "In GCP, 'us-east5' is a region and 'us-east5-b' is a zone" in response.text


def test_create_cluster_invalid_zone_format(client: TestClient):
    payload = {
        "cluster_name": "bad-zone-cluster",
        "zone": "not-a-zone",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 422
    assert "Invalid GCP zone" in response.text


def test_create_cluster_mismatched_zone_and_region(client: TestClient):
    payload = {
        "cluster_name": "mismatch-cluster",
        "zone": "us-central1-a",
        "region": "us-east5",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 422
    assert "does not match zone" in response.text


def test_create_cluster_auto_derives_region_from_zone(client: TestClient):
    payload = {
        "cluster_name": "gem-auto-derive",
        "project_id": "test-project-123",
        "zone": "us-east5-b",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 202


def test_create_cluster_swagger_string_placeholders_sanitized(client: TestClient):
    """Verify that Swagger default 'string' placeholders are cleaned and do not cause validation failure."""
    payload = {
        "cluster_name": "api-cluster-1",
        "project_id": "smooth-graph-498313-p2",
        "zone": "us-east5-b",
        "region": "us-east5",
        "provisioning_sa_email": "string",
        "gcp_cluster_admin_sa": "string",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 202


def test_create_cluster_invalid_sa_email_fails(client: TestClient):
    payload = {
        "cluster_name": "gem-bad-sa",
        "project_id": "smooth-graph-498313-p2",
        "zone": "us-east5-b",
        "provisioning_sa_email": "not-an-email-address",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 422
    assert "Must be a valid service account email" in response.text


def test_create_cluster_invalid_storage_size_fails(client: TestClient):
    payload = {
        "cluster_name": "gem-bad-storage",
        "project_id": "smooth-graph-498313-p2",
        "zone": "us-east5-b",
        "node_storage_size": "100",
    }
    response = client.post("/api/v1/clusters/create", json=payload)
    assert response.status_code == 422
    assert "Invalid node_storage_size" in response.text
