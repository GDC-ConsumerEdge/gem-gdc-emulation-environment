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

import pytest

from gem_api.config import get_settings
from gem_api.models.clusters import ClusterCreateRequest, ClusterDeleteRequest
from gem_api.models.edge_router import EdgeRouterCreateRequest, EdgeRouterDeleteRequest
from gem_api.models.operations import OperationStatus, OperationType
from gem_api.models.workstation import (
    WorkstationCreateRequest,
    WorkstationDeleteRequest,
)
from gem_api.services.operations import get_operation_manager
from gem_api.services.runner import get_process_runner


@pytest.mark.asyncio
async def test_runner_cluster_create_mock():
    op_mgr = get_operation_manager()
    runner = get_process_runner()
    op_id = "test-runner-create"

    await op_mgr.register_operation(
        operation_id=op_id,
        operation_type=OperationType.CLUSTER_CREATE,
        target_resource=op_id,
    )

    req = ClusterCreateRequest(cluster_name=op_id, project_id="test-project-123")
    await runner.run_cluster_create(req, op_id)

    op = op_mgr.get_operation(op_id)
    assert op is not None
    assert op.status == OperationStatus.SUCCEEDED
    assert op.completed_at is not None

    logs = op_mgr.get_logs(op_id)
    assert any("Apply complete" in line for line in logs)


@pytest.mark.asyncio
async def test_runner_cluster_delete_mock():
    op_mgr = get_operation_manager()
    runner = get_process_runner()
    op_id = "test-runner-del"

    await op_mgr.register_operation(
        operation_id=op_id,
        operation_type=OperationType.CLUSTER_DELETE,
        target_resource=op_id,
    )

    req = ClusterDeleteRequest(cluster_name=op_id, project_id="test-project-123")
    await runner.run_cluster_delete(req, op_id)

    op = op_mgr.get_operation(op_id)
    assert op is not None
    assert op.status == OperationStatus.SUCCEEDED


@pytest.mark.asyncio
async def test_runner_workstation_pipeline():
    op_mgr = get_operation_manager()
    runner = get_process_runner()

    # Create
    await op_mgr.register_operation(
        operation_id="ws-op",
        operation_type=OperationType.WORKSTATION_CREATE,
        target_resource="gem-admin-ws",
    )
    await runner.run_workstation_create(
        WorkstationCreateRequest(project_id="test-project-123"), "ws-op"
    )
    assert op_mgr.get_operation("ws-op").status == OperationStatus.SUCCEEDED

    # Delete
    await op_mgr.register_operation(
        operation_id="ws-del-op",
        operation_type=OperationType.WORKSTATION_DELETE,
        target_resource="gem-admin-ws",
    )
    await runner.run_workstation_delete(
        WorkstationDeleteRequest(project_id="test-project-123"), "ws-del-op"
    )
    assert op_mgr.get_operation("ws-del-op").status == OperationStatus.SUCCEEDED


@pytest.mark.asyncio
async def test_runner_edge_router_pipeline():
    op_mgr = get_operation_manager()
    runner = get_process_runner()

    # Create
    await op_mgr.register_operation(
        operation_id="er-op",
        operation_type=OperationType.EDGE_ROUTER_CREATE,
        target_resource="gem-edge-router",
    )
    await runner.run_edge_router_create(
        EdgeRouterCreateRequest(project_id="test-project-123"), "er-op"
    )
    assert op_mgr.get_operation("er-op").status == OperationStatus.SUCCEEDED

    # Delete
    await op_mgr.register_operation(
        operation_id="er-del-op",
        operation_type=OperationType.EDGE_ROUTER_DELETE,
        target_resource="gem-edge-router",
    )
    await runner.run_edge_router_delete(
        EdgeRouterDeleteRequest(project_id="test-project-123"), "er-del-op"
    )
    assert op_mgr.get_operation("er-del-op").status == OperationStatus.SUCCEEDED


@pytest.mark.asyncio
async def test_runner_cluster_delete_command_generation(
    monkeypatch: pytest.MonkeyPatch,
):
    """Verify that cluster delete sets CLUSTER_NAME, sanitizes Swagger placeholders, and passes -var flags."""
    op_mgr = get_operation_manager()
    runner = get_process_runner()
    op_id = "pr33"

    monkeypatch.setattr(runner, "_is_mock_enabled", lambda: False)

    executed_commands: list[dict] = []

    async def mock_execute(
        operation_id: str,
        cmd: list[str],
        cwd: any,
        env: dict[str, str],
        step_name: str,
        step_message: str,
    ) -> None:
        executed_commands.append(
            {
                "operation_id": operation_id,
                "cmd": cmd,
                "cwd": cwd,
                "env": env,
                "step_name": step_name,
            }
        )

    monkeypatch.setattr(runner, "_execute_command", mock_execute)

    await op_mgr.register_operation(
        operation_id=op_id,
        operation_type=OperationType.CLUSTER_DELETE,
        target_resource=op_id,
    )

    # Request with OpenAPI default "string" placeholders
    req = ClusterDeleteRequest(
        cluster_name="pr33",
        project_id="string",
        zone="string",
        tf_state_bucket="string",
    )
    await runner.run_cluster_delete(req, op_id)

    assert len(executed_commands) == 3

    settings = get_settings()
    # Step 1: Ansible cleanup
    ansible_step = executed_commands[0]
    assert ansible_step["cmd"][0] == "ansible-playbook"
    assert "cleanup.yaml" in ansible_step["cmd"]
    assert ansible_step["env"]["CLUSTER_NAME"] == "pr33"
    assert ansible_step["env"]["PROJECT_ID"] == settings.default_project_id
    assert ansible_step["env"]["GEM_GCP_ZONE"] == settings.default_zone

    # Step 2: Terraform init
    tf_init_step = executed_commands[1]
    assert tf_init_step["cmd"][0:2] == ["terraform", "init"]
    assert (
        f"-backend-config=bucket={settings.get_tf_state_bucket()}"
        in tf_init_step["cmd"]
    )
    assert "-backend-config=prefix=clusters/pr33/state" in tf_init_step["cmd"]

    # Step 3: Terraform destroy
    tf_destroy_step = executed_commands[2]
    assert tf_destroy_step["cmd"][0:2] == ["terraform", "destroy"]
    assert f"-var=project_id={settings.default_project_id}" in tf_destroy_step["cmd"]
    assert "-var=cluster_name=pr33" in tf_destroy_step["cmd"]
    assert f"-var=zone={settings.default_zone}" in tf_destroy_step["cmd"]
    assert f"-var=region={settings.default_region}" in tf_destroy_step["cmd"]


@pytest.mark.asyncio
async def test_runner_cluster_create_command_generation(
    monkeypatch: pytest.MonkeyPatch,
):
    """Verify that cluster create sets CLUSTER_NAME, canonical prefix, and apply arguments."""
    op_mgr = get_operation_manager()
    runner = get_process_runner()
    op_id = "gem-c1"

    monkeypatch.setattr(runner, "_is_mock_enabled", lambda: False)

    executed_commands: list[dict] = []

    async def mock_execute(
        operation_id: str,
        cmd: list[str],
        cwd: any,
        env: dict[str, str],
        step_name: str,
        step_message: str,
    ) -> None:
        executed_commands.append({"cmd": cmd, "env": env})

    monkeypatch.setattr(runner, "_execute_command", mock_execute)

    await op_mgr.register_operation(
        operation_id=op_id,
        operation_type=OperationType.CLUSTER_CREATE,
        target_resource=op_id,
    )

    req = ClusterCreateRequest(
        cluster_name="gem-c1",
        project_id="my-custom-proj",
        zone="us-east1-b",
        provisioning_sa_email="sa@my-custom-proj.iam.gserviceaccount.com",
    )
    await runner.run_cluster_create(req, op_id)

    assert len(executed_commands) == 3

    # Terraform init
    init_cmd = executed_commands[0]["cmd"]
    assert "-backend-config=bucket=gem-my-custom-proj-tfstate" in init_cmd
    assert "-backend-config=prefix=clusters/gem-c1/state" in init_cmd
    assert (
        "-backend-config=impersonate_service_account=sa@my-custom-proj.iam.gserviceaccount.com"
        in init_cmd
    )

    # Terraform apply
    apply_cmd = executed_commands[1]["cmd"]
    assert apply_cmd[0:2] == ["terraform", "apply"]
    assert "-var=project_id=my-custom-proj" in apply_cmd
    assert "-var=cluster_name=gem-c1" in apply_cmd
    assert "-var=zone=us-east1-b" in apply_cmd
    assert (
        "-var=provisioning_sa_email=sa@my-custom-proj.iam.gserviceaccount.com"
        in apply_cmd
    )

    # Ansible create-cluster
    ansible_cmd = executed_commands[2]["cmd"]
    assert ansible_cmd[0] == "ansible-playbook"
    assert "create-cluster.yaml" in ansible_cmd
    assert executed_commands[2]["env"]["CLUSTER_NAME"] == "gem-c1"
    assert (
        executed_commands[2]["env"]["GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"]
        == "sa@my-custom-proj.iam.gserviceaccount.com"
    )


@pytest.mark.asyncio
async def test_runner_workstation_and_edge_router_prefixes(
    monkeypatch: pytest.MonkeyPatch,
):
    """Verify workstation and edge router use canonical state prefixes and pass vars."""
    runner = get_process_runner()
    monkeypatch.setattr(runner, "_is_mock_enabled", lambda: False)

    executed_commands: list[dict] = []

    async def mock_execute(
        operation_id: str,
        cmd: list[str],
        cwd: any,
        env: dict[str, str],
        step_name: str,
        step_message: str,
    ) -> None:
        executed_commands.append({"cmd": cmd, "env": env})

    monkeypatch.setattr(runner, "_execute_command", mock_execute)

    # Workstation delete
    await runner.run_workstation_delete(
        WorkstationDeleteRequest(project_id="test-proj"), "ws-del"
    )
    assert (
        "-backend-config=prefix=admin-workstation/state" in executed_commands[0]["cmd"]
    )
    assert "-var=project_id=test-proj" in executed_commands[1]["cmd"]

    # Edge router delete
    executed_commands.clear()
    await runner.run_edge_router_delete(
        EdgeRouterDeleteRequest(project_id="test-proj", edge_router_name="gem-er"),
        "er-del",
    )
    assert "-backend-config=prefix=edge-router/state" in executed_commands[0]["cmd"]
    assert "-var=project_id=test-proj" in executed_commands[1]["cmd"]
    assert "-var=edge_router_name=gem-er" in executed_commands[1]["cmd"]
