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

import json

from fastapi.testclient import TestClient

from gem_api.models.configsync import RootSyncItem, RootSyncListResponse
from gem_api.models.vms import VirtualMachineItem, VirtualMachineListResponse
from gem_api.services.k8s_client import get_k8s_service


def test_get_cluster_status_disconnected(client: TestClient):
    k8s_service = get_k8s_service()

    async def mock_exec(
        cluster_name: str,
        args: list[str],
        input_data: str | None = None,
        timeout: float = 6.0,
    ):
        _ = (cluster_name, args, input_data, timeout)
        return -1, "", "cluster unreachable"

    orig_exec = k8s_service._exec_kubectl
    k8s_service._exec_kubectl = mock_exec
    try:
        response = client.get("/api/v1/clusters/gem-nonexistent-cluster/status")
        assert response.status_code == 200
        data = response.json()
        assert data["connected"] is False
        assert data["mode"] == "Disconnected"
        assert data["nodes"] == []
    finally:
        k8s_service._exec_kubectl = orig_exec


def test_get_cluster_status_connected(client: TestClient):
    k8s_service = get_k8s_service()
    mock_nodes_json = json.dumps(
        {
            "items": [
                {
                    "metadata": {
                        "name": "gem-cluster-1-node1",
                        "labels": {"node-role.kubernetes.io/control-plane": "true"},
                    },
                    "status": {
                        "conditions": [{"type": "Ready", "status": "True"}],
                        "addresses": [{"type": "InternalIP", "address": "10.200.54.2"}],
                        "allocatable": {"cpu": "32", "memory": "64Gi"},
                    },
                }
            ]
        }
    )

    async def mock_exec(
        cluster_name: str,
        args: list[str],
        input_data: str | None = None,
        timeout: float = 6.0,
    ):
        _ = (cluster_name, input_data, timeout)
        if "nodes" in args:
            return 0, mock_nodes_json, ""
        return -1, "", "unknown command"

    orig_exec = k8s_service._exec_kubectl
    k8s_service._exec_kubectl = mock_exec
    try:
        response = client.get("/api/v1/clusters/gem-cluster-1/status")
        assert response.status_code == 200
        data = response.json()
        assert data["connected"] is True
        assert data["mode"] == "Live Connected"
        assert len(data["nodes"]) == 1
        assert data["nodes"][0]["name"] == "gem-cluster-1-node1"
        assert data["nodes"][0]["status"] == "Ready"
    finally:
        k8s_service._exec_kubectl = orig_exec


def test_list_secondary_networks_from_manifest(client: TestClient):
    k8s_service = get_k8s_service()

    async def mock_exec(
        cluster_name: str,
        args: list[str],
        input_data: str | None = None,
        timeout: float = 6.0,
    ):
        _ = (cluster_name, args, input_data, timeout)
        return -1, "", "network CRDs not installed"

    orig_exec = k8s_service._exec_kubectl
    k8s_service._exec_kubectl = mock_exec
    try:
        response = client.get("/api/v1/clusters/gem-cluster-1/networks")
        assert response.status_code == 200
        data = response.json()
        assert "networks" in data
        assert len(data["networks"]) >= 1
        assert data["networks"][0]["vlan_id"] == 123
        assert data["networks"][0]["interface_name"] == "gdcenet0.123"
    finally:
        k8s_service._exec_kubectl = orig_exec


def test_list_vms_empty(client: TestClient):
    response = client.get("/api/v1/clusters/gem-cluster-1/vms")
    assert response.status_code == 200
    data = response.json()
    assert "vms" in data
    assert isinstance(data["vms"], list)


def test_list_vms_with_results(client: TestClient):
    k8s_service = get_k8s_service()

    async def mock_list_vms(
        cluster_name: str, namespace: str | None = None, project_id: str | None = None
    ):
        _ = (cluster_name, namespace, project_id)
        return VirtualMachineListResponse(
            vms=[
                VirtualMachineItem(
                    name="vm-edge-01",
                    namespace="default",
                    status="Running",
                    cpus=2,
                    memory="4Gi",
                    ip="10.240.1.50",
                    image="ubuntu-22.04",
                    uptime="2h",
                    power_state="Running",
                )
            ]
        )

    orig_method = k8s_service.list_vms
    k8s_service.list_vms = mock_list_vms
    try:
        response = client.get("/api/v1/clusters/gem-cluster-1/vms")
        assert response.status_code == 200
        data = response.json()
        assert len(data["vms"]) == 1
        assert data["vms"][0]["name"] == "vm-edge-01"
    finally:
        k8s_service.list_vms = orig_method


def test_vm_deploy_and_power_and_delete(client: TestClient):
    cluster = "gem-test-cluster"
    deploy_payload = {
        "name": "test-vm-01",
        "namespace": "default",
        "cpus": 2,
        "memory": "4Gi",
        "image": "ubuntu-22.04-server-cloudimg-amd64",
    }
    r_deploy = client.post(f"/api/v1/clusters/{cluster}/vms", json=deploy_payload)
    assert r_deploy.status_code == 201
    assert r_deploy.json()["name"] == "test-vm-01"

    r_power = client.post(
        f"/api/v1/clusters/{cluster}/vms/test-vm-01/power",
        json={"namespace": "default", "running": True},
    )
    assert r_power.status_code == 200
    assert r_power.json()["power_state"] == "Running"

    r_del = client.delete(
        f"/api/v1/clusters/{cluster}/vms/test-vm-01?namespace=default"
    )
    assert r_del.status_code == 200
    assert r_del.json()["success"] is True


def test_list_rootsyncs_empty(client: TestClient):
    response = client.get("/api/v1/clusters/gem-cluster-1/configsync")
    assert response.status_code == 200
    data = response.json()
    assert "root_syncs" in data
    assert isinstance(data["root_syncs"], list)


def test_list_rootsyncs_with_results(client: TestClient):
    k8s_service = get_k8s_service()

    async def mock_rootsyncs(cluster_name: str, project_id: str | None = None):
        _ = (cluster_name, project_id)
        return RootSyncListResponse(
            root_syncs=[
                RootSyncItem(
                    name="root-sync-core",
                    namespace="config-management-system",
                    repo="https://github.com/org/repo.git",
                    branch="main",
                    dir="/manifests",
                    auth="none",
                    period="15s",
                    status="SYNCED",
                    commit="abc1234",
                    last_synced="2026-08-24T12:00:00Z",
                    message="Sync ok",
                )
            ]
        )

    orig_method = k8s_service.list_rootsyncs
    k8s_service.list_rootsyncs = mock_rootsyncs
    try:
        response = client.get("/api/v1/clusters/gem-cluster-1/configsync")
        assert response.status_code == 200
        data = response.json()
        assert len(data["root_syncs"]) == 1
        assert data["root_syncs"][0]["name"] == "root-sync-core"
    finally:
        k8s_service.list_rootsyncs = orig_method


def test_pod_crud_lifecycle(client: TestClient):
    cluster = "gem-test-pod-cluster"
    create_payload = {
        "name": "test-pod-app",
        "namespace": "default",
        "image": "nginx:alpine",
        "port": 80,
    }
    r_create = client.post(f"/api/v1/clusters/{cluster}/pods", json=create_payload)
    assert r_create.status_code == 201
    assert r_create.json()["name"] == "test-pod-app"

    r_del = client.delete(
        f"/api/v1/clusters/{cluster}/pods/test-pod-app?namespace=default"
    )
    assert r_del.status_code == 200
    assert r_del.json()["success"] is True
