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
import json
import subprocess
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from fastapi import HTTPException

from gem_api.config import (
    Settings,
    _resolve_default_project,
    _resolve_default_zone,
)
from gem_api.manifest import (
    get_abm_version,
    get_default_hardware_variant,
    get_default_secondary_networks,
    get_valid_gdc_versions,
    get_valid_hardware_variants,
    load_group_vars,
)
from gem_api.models.clusters import (
    ClusterCreateRequest,
    ClusterDeleteRequest,
    SecondaryNetworkConfig,
)
from gem_api.models.edge_router import EdgeRouterCreateRequest, EdgeRouterDeleteRequest
from gem_api.models.operations import OperationStatus, OperationType
from gem_api.models.workstation import (
    WorkstationCreateRequest,
    WorkstationDeleteRequest,
)
from gem_api.services.gcp_client import GcpService
from gem_api.services.k8s_client import MISSING_KUBECTL, K8sService
from gem_api.services.operations import OperationManager
from gem_api.services.process import communicate_or_kill
from gem_api.services.runner import (
    ProcessRunner,
    _clean_str,
    _resolve_zone_and_region,
)


def test_config_resolve_defaults_via_gcloud(monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify _resolve_default_project and _resolve_default_zone fall back to gcloud."""
    monkeypatch.delenv("PROJECT_ID", raising=False)
    monkeypatch.delenv("GCP_PROJECT", raising=False)
    monkeypatch.delenv("GEM_GCP_ZONE", raising=False)
    monkeypatch.delenv("CLOUDSDK_COMPUTE_ZONE", raising=False)

    with (
        patch("gem_api.config.shutil.which", return_value="/usr/bin/gcloud"),
        patch(
            "gem_api.config.subprocess.run",
            return_value=SimpleNamespace(returncode=0, stdout="gcloud-proj-123\n"),
        ),
    ):
        assert _resolve_default_project() == "gcloud-proj-123"

    with (
        patch("gem_api.config.shutil.which", return_value="/usr/bin/gcloud"),
        patch(
            "gem_api.config.subprocess.run",
            return_value=SimpleNamespace(returncode=0, stdout="europe-west1-b\n"),
        ),
    ):
        assert _resolve_default_zone() == "europe-west1-b"

    # Subprocess error falls back to static defaults
    with (
        patch("gem_api.config.shutil.which", return_value="/usr/bin/gcloud"),
        patch(
            "gem_api.config.subprocess.run",
            side_effect=subprocess.SubprocessError("boom"),
        ),
    ):
        assert _resolve_default_project() == "gem-default-project"
        assert _resolve_default_zone() == "us-central1-a"

    settings = Settings(default_zone="nohyphenzone")
    assert settings.default_region == "us-central1"


def test_manifest_edge_cases(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    """Verify manifest helper edge cases and error handling."""
    load_group_vars.cache_clear()

    # Unknown GDC version returns None
    assert get_abm_version("0.0.0-nonexistent") is None

    # Invalid YAML file raises RuntimeError
    bad_yaml = tmp_path / "bad.yaml"
    bad_yaml.write_text(":::invalid_yaml\n\t[\n", encoding="utf-8")
    monkeypatch.setenv("GEM_GROUP_VARS_PATH", str(bad_yaml))
    load_group_vars.cache_clear()
    with pytest.raises(RuntimeError, match="Failed to parse Ansible group_vars"):
        load_group_vars()

    # Missing file raises RuntimeError
    monkeypatch.setenv("GEM_GROUP_VARS_PATH", str(tmp_path / "missing.yaml"))
    load_group_vars.cache_clear()
    with pytest.raises(RuntimeError, match="Could not locate"):
        load_group_vars()

    # Non-dict / empty fields in group_vars
    custom_yaml = tmp_path / "custom.yaml"
    custom_yaml.write_text(
        "emulated_gdc_versions: null\nhardware_variants: []\nsecondary_networks: invalid\n",
        encoding="utf-8",
    )
    monkeypatch.setenv("GEM_GROUP_VARS_PATH", str(custom_yaml))
    load_group_vars.cache_clear()

    assert get_abm_version("1.15") is None
    assert get_default_secondary_networks() == []
    with pytest.raises(ValueError, match="emulated_gdc_versions"):
        get_valid_gdc_versions()
    with pytest.raises(ValueError, match="hardware_variants"):
        get_valid_hardware_variants()

    # Fallback to first hardware variant when default_hardware_variant is unset
    fallback_yaml = tmp_path / "fallback.yaml"
    fallback_yaml.write_text(
        "hardware_variants:\n  - custom-variant-1\n",
        encoding="utf-8",
    )
    monkeypatch.setenv("GEM_GROUP_VARS_PATH", str(fallback_yaml))
    load_group_vars.cache_clear()
    assert get_default_hardware_variant() == "custom-variant-1"

    monkeypatch.delenv("GEM_GROUP_VARS_PATH", raising=False)
    load_group_vars.cache_clear()


async def test_communicate_or_kill_timeout() -> None:
    """Verify communicate_or_kill kills hung processes and handles reap timeout."""
    proc = MagicMock()
    proc.pid = 99999

    async def slow_communicate(_input: bytes | None = None) -> tuple[bytes, bytes]:
        await asyncio.sleep(10)
        return b"", b""

    async def slow_wait() -> int:
        await asyncio.sleep(10)
        return -9

    proc.communicate = slow_communicate
    proc.wait = slow_wait

    with (
        patch("gem_api.services.process._REAP_TIMEOUT", 0.01),
        pytest.raises(TimeoutError),
    ):
        await communicate_or_kill(proc, timeout=0.01)

    proc.kill.assert_called_once()


async def test_gcp_service_list_projects_and_clusters() -> None:
    """Verify GcpService parses gcloud output and handles CLI errors gracefully."""
    svc = GcpService()

    # 1. list_projects success
    fake_proc = MagicMock()
    fake_proc.returncode = 0
    with (
        patch(
            "gem_api.services.gcp_client.shutil.which", return_value="/usr/bin/gcloud"
        ),
        patch(
            "gem_api.services.gcp_client.asyncio.create_subprocess_exec",
            AsyncMock(return_value=fake_proc),
        ),
        patch(
            "gem_api.services.gcp_client.communicate_or_kill",
            AsyncMock(
                return_value=(
                    json.dumps([{"projectId": "p1", "name": "Proj 1"}]).encode(),
                    b"",
                )
            ),
        ),
    ):
        res = await svc.list_projects(limit=10)
        assert len(res.projects) == 1
        assert res.projects[0].project_id == "p1"

    # 2. list_projects error fallback
    with (
        patch(
            "gem_api.services.gcp_client.shutil.which", return_value="/usr/bin/gcloud"
        ),
        patch(
            "gem_api.services.gcp_client.asyncio.create_subprocess_exec",
            AsyncMock(side_effect=OSError("gcloud failed")),
        ),
    ):
        res_err = await svc.list_projects()
        assert res_err.projects == []

        # 3. list_clusters returns only RUNNING fleet memberships
    fleet_json = json.dumps(
        [
            {
                "name": "projects/p1/locations/global/memberships/c1",
                "monitoringConfig": {"location": "us-central1-a"},
                "endpoint": {
                    "kubernetesMetadata": {
                        "kubernetesApiServerVersion": "1.32.0",
                        "nodeCount": 3,
                    }
                },
                "state": {"code": "READY"},
                "createTime": "2026-01-01T00:00:00Z",
            },
            {
                "name": "projects/p1/locations/global/memberships/c2",
                "monitoringConfig": {"location": "us-central1-a"},
                "state": {"code": "CREATING"},
            },
        ]
    ).encode()

    with (
        patch(
            "gem_api.services.gcp_client.shutil.which", return_value="/usr/bin/gcloud"
        ),
        patch(
            "gem_api.services.gcp_client.asyncio.create_subprocess_exec",
            AsyncMock(return_value=fake_proc),
        ) as mock_exec_gcloud,
        patch(
            "gem_api.services.gcp_client.communicate_or_kill",
            AsyncMock(return_value=(fleet_json, b"")),
        ),
    ):
        clusters_res = await svc.list_clusters(project_id="p1")
        by_name = {c.name: c for c in clusters_res.clusters}
        assert set(by_name) == {"c1"}
        assert by_name["c1"].status == "RUNNING"
        mock_exec_gcloud.assert_awaited_once()


async def test_k8s_service_exec_and_parsers(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Verify K8sService subprocess execution, error mapping, and live JSON parsers."""
    svc = K8sService()

    # _find_kubeconfig with KUBECONFIG env var
    kc = tmp_path / "kubeconfig"
    kc.write_text("apiVersion: v1\n", encoding="utf-8")
    monkeypatch.setenv("KUBECONFIG", str(kc))
    assert svc._find_kubeconfig("c1") == str(kc)

    # _exec_kubectl success, TimeoutError, OSError, and missing binary
    with patch("gem_api.services.k8s_client.shutil.which", return_value=None):
        rc, _, err = await svc._exec_kubectl("c1", ["get", "nodes"])
        assert rc == -1 and err == MISSING_KUBECTL

    fake_proc = MagicMock()
    fake_proc.returncode = 0
    with (
        patch(
            "gem_api.services.k8s_client.shutil.which", return_value="/usr/bin/kubectl"
        ),
        patch(
            "gem_api.services.k8s_client.asyncio.create_subprocess_exec",
            AsyncMock(return_value=fake_proc),
        ),
        patch(
            "gem_api.services.k8s_client.communicate_or_kill",
            AsyncMock(return_value=(b'{"items": []}', b"")),
        ),
    ):
        rc, out, _ = await svc._exec_kubectl(
            "c1", ["apply", "-f", "-"], input_data="kind: Pod"
        )
        assert rc == 0 and out == '{"items": []}'

    with (
        patch(
            "gem_api.services.k8s_client.shutil.which", return_value="/usr/bin/kubectl"
        ),
        patch(
            "gem_api.services.k8s_client.asyncio.create_subprocess_exec",
            AsyncMock(side_effect=TimeoutError()),
        ),
    ):
        rc, _, err = await svc._exec_kubectl("c1", ["get", "pods"])
        assert rc == -1 and "timed out" in err

    with (
        patch(
            "gem_api.services.k8s_client.shutil.which", return_value="/usr/bin/kubectl"
        ),
        patch(
            "gem_api.services.k8s_client.asyncio.create_subprocess_exec",
            AsyncMock(side_effect=OSError("exec err")),
        ),
    ):
        rc, _, err = await svc._exec_kubectl("c1", ["get", "pods"])
        assert rc == -1 and "exec err" in err

    # Live parsers: get_cluster_status, list_secondary_networks, list_rootsyncs, list_pods, delete_pod
    nodes_payload = json.dumps(
        {
            "items": [
                {
                    "metadata": {
                        "name": "c1-node1",
                        "labels": {"node-role.kubernetes.io/control-plane": ""},
                    },
                    "status": {
                        "conditions": [{"type": "Ready", "status": "True"}],
                        "addresses": [{"type": "InternalIP", "address": "10.200.0.2"}],
                    },
                }
            ]
        }
    )
    with patch.object(
        svc, "_exec_kubectl", AsyncMock(return_value=(0, nodes_payload, ""))
    ):
        st = await svc.get_cluster_status("c1")
        assert st.connected is True
        assert len(st.nodes) == 1
        assert st.nodes[0].role == "Control Plane"

    nets_payload = json.dumps(
        {
            "items": [
                {
                    "metadata": {"name": "pod-network"},
                    "spec": {"type": "L3"},
                },
                {
                    "metadata": {
                        "name": "vlan-123",
                        "annotations": {
                            "networking.gke.io/gdce-vlan-id": "invalid-int",
                            "networking.gke.io/gdce-lb-service-vip-cidrs": "172.16.12.200/32",
                        },
                    },
                    "spec": {
                        "type": "L2",
                        "gateway4": "172.16.12.1",
                        "l2NetworkConfig": {"prefixLength4": 24},
                    },
                },
            ]
        }
    )
    with patch.object(
        svc, "_exec_kubectl", AsyncMock(return_value=(0, nets_payload, ""))
    ):
        nets = await svc.list_secondary_networks("c1")
        assert len(nets.networks) == 1
        assert nets.networks[0].name == "vlan-123"
        assert nets.networks[0].vlan_id == 0
        assert nets.networks[0].gateway == "172.16.12.1"
        assert nets.networks[0].subnet == "172.16.12.0/24"

    rs_payload = json.dumps(
        {
            "items": [
                {
                    "metadata": {
                        "name": "root-sync",
                        "namespace": "config-management-system",
                    },
                    "spec": {"git": {"repo": "https://example.com/repo.git"}},
                    "status": {
                        "sync": {"status": "SYNCED", "commit": "1234567890abcdef"}
                    },
                }
            ]
        }
    )
    with patch.object(
        svc, "_exec_kubectl", AsyncMock(return_value=(0, rs_payload, ""))
    ):
        rs = await svc.list_rootsyncs("c1")
        assert len(rs.root_syncs) == 1
        assert rs.root_syncs[0].commit == "123456789"

    pods_payload = json.dumps(
        {
            "items": [
                {
                    "metadata": {"name": "web-1", "namespace": "prod"},
                    "spec": {"nodeName": "c1-node1"},
                    "status": {
                        "phase": "Running",
                        "podIP": "10.0.1.5",
                        "containerStatuses": [
                            {
                                "name": "nginx",
                                "image": "nginx:latest",
                                "ready": True,
                                "restartCount": 2,
                                "state": {"running": {}},
                            },
                            {
                                "name": "init-done",
                                "image": "busybox:latest",
                                "ready": False,
                                "restartCount": 0,
                                "state": {"terminated": {"exitCode": 0}},
                            },
                        ],
                    },
                }
            ]
        }
    )
    with patch.object(
        svc, "_exec_kubectl", AsyncMock(return_value=(0, pods_payload, ""))
    ):
        pods = await svc.list_pods("c1", namespace="prod", label_selector="app=web")
        assert len(pods.pods) == 1
        assert pods.pods[0].restarts == 2
        assert pods.pods[0].ready == "1/2"
        assert pods.pods[0].containers[1].state == "terminated"

    with patch.object(
        svc, "_exec_kubectl", AsyncMock(return_value=(0, "", ""))
    ) as mock_del:
        del_res = await svc.delete_pod(
            "c1", "web-1", namespace="prod", grace_period_seconds=0
        )
        assert del_res.success is True
        mock_del.assert_awaited_once_with(
            "c1", ["delete", "pod", "web-1", "-n", "prod", "--grace-period=0"]
        )


async def test_operation_manager_logs_queue_and_cancel() -> None:
    """Verify OperationManager log tailing, full queue handling, and process cancellation."""
    mgr = OperationManager()
    rec = await mgr.register_operation(
        "op-unit-1", OperationType.CLUSTER_CREATE, "cluster-unit-1"
    )
    mgr.append_log("op-unit-1", "line 1")
    mgr.append_log("op-unit-1", "line 2")

    # Path traversal in operation_id raises 400
    with pytest.raises(HTTPException) as traversal_exc:
        mgr.get_logs("../secret")
    assert traversal_exc.value.status_code == 400

    # Single-char invalid cluster name and IPv6 CIDR/gateway raise ValueError
    with pytest.raises(ValueError, match="cluster_name"):
        ClusterCreateRequest(cluster_name="1")
    with pytest.raises(ValueError, match="IPv4"):
        SecondaryNetworkConfig(
            name="vlan-v6",
            vlan_id=10,
            subnet="2001:db8::/64",
            gateway="172.16.0.1",
            vip_pool="172.16.0.10-172.16.0.20",
        )

    # Tail logs from file
    tailed = mgr.get_logs("op-unit-1", tail=1)
    assert len(tailed) == 1 and "line 2" in tailed[0]

    # Fallback to memory buffer when file does not exist
    log_path = mgr._get_log_file_path("op-unit-1")
    log_path.unlink(missing_ok=True)
    mem_tailed = mgr.get_logs("op-unit-1", tail=1)
    assert len(mem_tailed) == 1 and "line 2" in mem_tailed[0]

    # Unknown operation raises 404
    with pytest.raises(HTTPException) as exc:
        mgr.get_logs("unknown-op")
    assert exc.value.status_code == 404

    # Full subscriber queue drops oldest entry without blocking
    q: asyncio.Queue[str | None] = asyncio.Queue(maxsize=2)
    q.put_nowait("old-1")
    q.put_nowait("old-2")
    rec.subscribers.append(q)
    mgr._publish(rec, "new-3")
    assert q.get_nowait() == "old-2"
    assert q.get_nowait() == "new-3"

    # Cancel running operation with attached fake process and task
    fake_proc = MagicMock()
    fake_proc.returncode = None
    fake_proc.pid = 42424
    rec.process = fake_proc
    dummy_task = asyncio.create_task(asyncio.sleep(30))
    rec.task = dummy_task

    with (
        patch("gem_api.services.operations.os.getpgid", return_value=42424),
        patch("gem_api.services.operations.os.killpg") as mock_killpg,
    ):
        cancel_res = await mgr.cancel_operation("op-unit-1")
        assert cancel_res.success is True
        mock_killpg.assert_called_once()

    # Cancelling an already finished operation returns success=False
    cancel_again = await mgr.cancel_operation("op-unit-1")
    assert cancel_again.success is False


async def test_runner_execute_command_and_non_mock_pipelines(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """Verify ProcessRunner._execute_command streaming and non-mock Terraform/Ansible pipelines."""
    monkeypatch.setenv("GEM_MOCK_RUNNER", "false")
    assert _clean_str("  string ") is None
    assert _clean_str("my-val") == "my-val"
    # Swapped zone and region auto-correction
    z, r = _resolve_zone_and_region(
        "us-central1", "us-central1-a", "us-east1-b", "us-east1"
    )
    assert z == "us-central1-a" and r == "us-central1"

    mgr = OperationManager()
    runner = ProcessRunner(operation_manager=mgr)

    # 1. Test _execute_command streaming stdout lines and non-zero exit
    await mgr.register_operation("op-exec", OperationType.CLUSTER_CREATE, "c-exec")

    fake_proc = MagicMock()
    fake_proc.stdout = MagicMock()
    fake_proc.stdout.read = AsyncMock(
        side_effect=[
            b"google_compute_instance.node: Creating...\nTASK [vxlan : Setup] ***\n",
            b"trailing line without newline",
            b"",
        ]
    )
    fake_proc.wait = AsyncMock(return_value=0)

    with patch(
        "gem_api.services.runner.asyncio.create_subprocess_exec",
        AsyncMock(return_value=fake_proc),
    ):
        await runner._execute_command(
            "op-exec",
            ["echo", "test"],
            tmp_path,
            {},
            "Step 1",
            "Running step 1",
        )

    logs = "\n".join(mgr.get_logs("op-exec"))
    assert "trailing line without newline" in logs

    # Non-zero exit code raises RuntimeError
    fake_proc.stdout.read = AsyncMock(return_value=b"")
    fake_proc.wait = AsyncMock(return_value=1)
    with (
        patch(
            "gem_api.services.runner.asyncio.create_subprocess_exec",
            AsyncMock(return_value=fake_proc),
        ),
        pytest.raises(RuntimeError, match="failed with return code 1"),
    ):
        await runner._execute_command(
            "op-exec", ["false"], tmp_path, {}, "Step Fail", "Failing"
        )

    # 2. Test non-mock pipelines (cluster, workstation, edge router create & delete)
    with patch.object(runner, "_execute_command", AsyncMock()) as mock_exec:
        await mgr.register_operation("op-c-create", OperationType.CLUSTER_CREATE, "c1")
        await runner.run_cluster_create(
            ClusterCreateRequest(
                cluster_name="c1",
                secondary_networks=[
                    SecondaryNetworkConfig(
                        name="vlan-123",
                        vlan_id=123,
                        subnet="172.16.12.0/24",
                        gateway="172.16.12.1",
                        vip_pool="172.16.12.200-172.16.12.250",
                    )
                ],
            ),
            "op-c-create",
        )
        assert mgr.get_operation("op-c-create").status == OperationStatus.SUCCEEDED

        await mgr.register_operation("op-c-del", OperationType.CLUSTER_DELETE, "c1-del")
        await runner.run_cluster_delete(
            ClusterDeleteRequest(cluster_name="c1-del"), "op-c-del"
        )
        assert mgr.get_operation("op-c-del").status == OperationStatus.SUCCEEDED

        await mgr.register_operation(
            "op-ws-create", OperationType.WORKSTATION_CREATE, "ws"
        )
        await runner.run_workstation_create(WorkstationCreateRequest(), "op-ws-create")
        assert mgr.get_operation("op-ws-create").status == OperationStatus.SUCCEEDED

        await mgr.register_operation(
            "op-ws-del", OperationType.WORKSTATION_DELETE, "ws-d"
        )
        await runner.run_workstation_delete(WorkstationDeleteRequest(), "op-ws-del")
        assert mgr.get_operation("op-ws-del").status == OperationStatus.SUCCEEDED

        await mgr.register_operation(
            "op-er-create", OperationType.EDGE_ROUTER_CREATE, "er"
        )
        await runner.run_edge_router_create(EdgeRouterCreateRequest(), "op-er-create")
        assert mgr.get_operation("op-er-create").status == OperationStatus.SUCCEEDED

        await mgr.register_operation(
            "op-er-del", OperationType.EDGE_ROUTER_DELETE, "er-d"
        )
        await runner.run_edge_router_delete(EdgeRouterDeleteRequest(), "op-er-del")
        assert mgr.get_operation("op-er-del").status == OperationStatus.SUCCEEDED
        assert mock_exec.await_count >= 12

    # 3. Test pipeline error recording when _execute_command raises
    with patch.object(
        runner, "_execute_command", AsyncMock(side_effect=RuntimeError("tf boom"))
    ):
        for op_id, coro in [
            (
                "err-c-del",
                runner.run_cluster_delete(
                    ClusterDeleteRequest(cluster_name="c-err"), "err-c-del"
                ),
            ),
            (
                "err-ws-create",
                runner.run_workstation_create(
                    WorkstationCreateRequest(), "err-ws-create"
                ),
            ),
            (
                "err-ws-del",
                runner.run_workstation_delete(WorkstationDeleteRequest(), "err-ws-del"),
            ),
            (
                "err-er-create",
                runner.run_edge_router_create(
                    EdgeRouterCreateRequest(), "err-er-create"
                ),
            ),
            (
                "err-er-del",
                runner.run_edge_router_delete(EdgeRouterDeleteRequest(), "err-er-del"),
            ),
        ]:
            await mgr.register_operation(
                op_id, OperationType.CLUSTER_DELETE, f"target-{op_id}"
            )
            await coro
            assert mgr.get_operation(op_id).status == OperationStatus.FAILED
