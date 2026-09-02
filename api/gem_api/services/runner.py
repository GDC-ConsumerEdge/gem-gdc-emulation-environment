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
import logging
import os
import subprocess
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from gem_api.config import get_settings
from gem_api.models.clusters import ClusterCreateRequest, ClusterDeleteRequest
from gem_api.models.edge_router import EdgeRouterCreateRequest, EdgeRouterDeleteRequest
from gem_api.models.operations import OperationStatus
from gem_api.models.validators import GCP_REGION_REGEX, GCP_ZONE_REGEX
from gem_api.models.workstation import (
    WorkstationCreateRequest,
    WorkstationDeleteRequest,
)
from gem_api.services.operations import OperationManager, get_operation_manager

logger = logging.getLogger("gem_api.runner")


def _clean_str(val: str | None) -> str | None:
    """Sanitize input string, returning None if empty or equal to Swagger placeholder."""
    if val is None:
        return None
    v = val.strip()
    if not v or v.lower() in ("string", "none", "null", "undefined"):
        return None
    return v


def _resolve_zone_and_region(
    raw_zone: str | None, raw_region: str | None, default_zone: str, default_region: str
) -> tuple[str, str]:
    """Resolve and sanitize zone and region, auto-correcting swapped values if encountered."""
    z = _clean_str(raw_zone) or default_zone
    r = _clean_str(raw_region)
    if z and r and GCP_REGION_REGEX.match(z) and GCP_ZONE_REGEX.match(r):
        z, r = r, z
    if not r:
        r = "-".join(z.split("-")[:-1]) if "-" in z else default_region
    return z, r


class ProcessRunner:
    """Orchestrates asynchronous execution of Terraform and Ansible CLI pipelines."""

    def __init__(self, operation_manager: OperationManager | None = None) -> None:
        self.op_mgr = operation_manager or get_operation_manager()

    def _is_mock_enabled(self) -> bool:
        return os.getenv("GEM_MOCK_RUNNER", "false").lower() in ("true", "1", "yes")

    async def _execute_command(
        self,
        operation_id: str,
        cmd: list[str],
        cwd: Path,
        env: dict[str, str],
        step_name: str,
        step_message: str,
    ) -> None:
        """Execute a subprocess command while streaming stdout/stderr into the operation logs."""
        record = self.op_mgr._operations.get(operation_id)
        if not record or record.status == OperationStatus.CANCELLED:
            logger.info(
                "Operation %s is cancelled or missing, skipping command %s",
                operation_id,
                cmd[0],
            )
            return

        await self.op_mgr.update_operation(
            operation_id=operation_id,
            status=OperationStatus.RUNNING,
            current_step=step_name,
            message=step_message,
        )

        self.op_mgr.append_log(
            operation_id,
            f"> Running: {' '.join(cmd)} (cwd: {cwd})",
        )

        try:
            process = await asyncio.create_subprocess_exec(
                *cmd,
                cwd=str(cwd),
                env=env,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.STDOUT,
                start_new_session=True,
            )
            record.process = process

            # Stream stdout line by line
            if process.stdout:
                while True:
                    line_bytes = await process.stdout.readline()
                    if not line_bytes:
                        break
                    line = line_bytes.decode("utf-8", errors="replace").rstrip()
                    if line:
                        self.op_mgr.append_log(operation_id, line)
                        # Dynamically update intermediate messages based on output
                        if "TASK [" in line:
                            task_name = line.split("TASK [", 1)[-1].rstrip("] *")
                            await self.op_mgr.update_operation(
                                operation_id=operation_id,
                                message=f"Ansible task: {task_name}",
                            )
                        elif (
                            "Creating..." in line
                            or "Modifying..." in line
                            or "Destroying..." in line
                        ):
                            await self.op_mgr.update_operation(
                                operation_id=operation_id,
                                message=f"Terraform resource update: {line.strip()}",
                            )

            return_code = await process.wait()

            if record.status == OperationStatus.CANCELLED:
                logger.info(
                    "Operation %s was cancelled during command %s", operation_id, cmd[0]
                )
                return

            if return_code != 0:
                err_msg = f"Command '{cmd[0]}' failed with return code {return_code}"
                self.op_mgr.append_log(operation_id, f"ERROR: {err_msg}")
                raise RuntimeError(err_msg)

        except (asyncio.CancelledError, KeyboardInterrupt):
            logger.info("Command execution task cancelled for %s", operation_id)
            raise
        except (RuntimeError, OSError, subprocess.SubprocessError, ValueError) as e:
            if record.status != OperationStatus.CANCELLED:
                self.op_mgr.append_log(operation_id, f"Execution exception: {e}")
            raise

    async def _mock_step(
        self,
        operation_id: str,
        step_name: str,
        step_message: str,
        sample_logs: list[str],
        delay: float = 0.05,
    ) -> None:
        """Simulate a deployment step for test suites and offline mock executions."""
        record = self.op_mgr._operations.get(operation_id)
        if not record or record.status == OperationStatus.CANCELLED:
            return

        await self.op_mgr.update_operation(
            operation_id=operation_id,
            status=OperationStatus.RUNNING,
            current_step=step_name,
            message=step_message,
        )

        for log in sample_logs:
            if record.status == OperationStatus.CANCELLED:
                return
            self.op_mgr.append_log(operation_id, log)
            if delay > 0:
                await asyncio.sleep(delay)

    # Cluster Create / Delete Pipelines

    async def run_cluster_create(
        self, request: ClusterCreateRequest, operation_id: str
    ) -> None:
        """Execute full cluster provisioning: Terraform apply -> Ansible create-cluster."""
        settings = get_settings()
        repo_root = settings.repo_root

        try:
            if self._is_mock_enabled():
                await self._mock_step(
                    operation_id,
                    "Terraform Provisioning (1/2)",
                    "Provisioning cluster compute instances and networking...",
                    [
                        "google_compute_instance.cluster_nodes[0]: Creating...",
                        "google_compute_instance.cluster_nodes[1]: Creating...",
                        "google_compute_instance.cluster_nodes[2]: Creating...",
                        "google_compute_instance.cluster_nodes: Creation complete after 35s",
                        "Apply complete! Resources: 6 added, 0 changed, 0 destroyed.",
                    ],
                )
                await self._mock_step(
                    operation_id,
                    "Ansible Configuration (2/2)",
                    "Configuring VXLAN overlay, TopoLVM storage, and GDC deployment...",
                    [
                        "PLAY [Configure GEM Cluster Nodes] **********************************",
                        "TASK [vxlan : Configure overlay network interfaces] *****************",
                        "TASK [topolvm : Setup LVM volume group and TopoLVM daemon] *********",
                        "TASK [gdc_deploy : Deploy Anthos Bare Metal cluster] ****************",
                        "PLAY RECAP **********************************************************",
                        f"{request.cluster_name}-node1 : ok=42   changed=18   unreachable=0    failed=0",
                    ],
                )
            else:
                tf_dir = repo_root / "terraform" / "cluster"
                ansible_dir = repo_root / "ansible"

                project_id = (
                    _clean_str(request.project_id) or settings.default_project_id
                )
                zone, region = _resolve_zone_and_region(
                    request.zone,
                    request.region,
                    settings.default_zone,
                    settings.default_region,
                )
                provisioning_sa = _clean_str(
                    request.provisioning_sa_email
                ) or settings.get_provisioning_sa(project_id)
                cluster_admin_sa = _clean_str(
                    request.gcp_cluster_admin_sa
                ) or settings.get_cluster_admin_sa(project_id)
                bucket = settings.get_tf_state_bucket(project_id)

                env = os.environ.copy()
                env["CLUSTER_NAME"] = request.cluster_name
                env["PROJECT_ID"] = project_id
                env["GEM_GCP_ZONE"] = zone
                env["TF_VAR_project_id"] = project_id
                env["TF_VAR_zone"] = zone
                env["TF_VAR_region"] = region
                env["TF_VAR_cluster_name"] = request.cluster_name
                env["TF_VAR_hardware_variant"] = request.hardware_variant
                env["TF_VAR_gce_network"] = request.gce_network
                env["TF_VAR_gce_subnetwork"] = request.gce_subnetwork
                env["TF_VAR_node_storage_size"] = request.node_storage_size
                env["TF_VAR_pod_cidr_blocks"] = request.pod_cidr_blocks
                env["TF_VAR_services_cidr_blocks"] = request.services_cidr_blocks
                env["TF_VAR_max_pods_per_node"] = str(request.max_pods_per_node)
                env["TF_VAR_emulate_gdc_version"] = request.emulate_gdc_version
                if provisioning_sa:
                    env["TF_VAR_provisioning_sa_email"] = provisioning_sa
                    env["GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"] = provisioning_sa
                if cluster_admin_sa:
                    env["TF_VAR_gcp_cluster_admin_sa"] = cluster_admin_sa

                # Step 1: Terraform Init & Apply
                init_cmd = [
                    "terraform",
                    "init",
                    "-input=false",
                    f"-backend-config=bucket={bucket}",
                    f"-backend-config=prefix=clusters/{request.cluster_name}/state",
                ]
                if provisioning_sa:
                    init_cmd.append(
                        f"-backend-config=impersonate_service_account={provisioning_sa}"
                    )

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=init_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Init (1/2)",
                    step_message="Initializing Terraform backend and providers...",
                )

                apply_cmd = [
                    "terraform",
                    "apply",
                    "-input=false",
                    "-auto-approve",
                    f"-var=project_id={project_id}",
                    f"-var=cluster_name={request.cluster_name}",
                    f"-var=zone={zone}",
                    f"-var=region={region}",
                    f"-var=hardware_variant={request.hardware_variant}",
                    f"-var=gce_network={request.gce_network}",
                    f"-var=gce_subnetwork={request.gce_subnetwork}",
                    f"-var=node_storage_size={request.node_storage_size}",
                ]
                if provisioning_sa:
                    apply_cmd.append(f"-var=provisioning_sa_email={provisioning_sa}")

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=apply_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Provisioning (1/2)",
                    step_message="Applying Terraform infrastructure resources...",
                )

                # Step 2: Ansible Cluster Deployment
                extra_vars: dict[str, Any] = {
                    "cluster_name": request.cluster_name,
                    "project_id": project_id,
                    "zone": zone,
                    "region": region,
                    "emulate_gdc_version": request.emulate_gdc_version,
                    "node_storage_size": request.node_storage_size,
                }
                if request.secondary_networks:
                    extra_vars["secondary_networks"] = [
                        net.model_dump() for net in request.secondary_networks
                    ]

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=[
                        "ansible-playbook",
                        "-i",
                        "inventory.sh",
                        "create-cluster.yaml",
                        "-e",
                        json.dumps(extra_vars),
                    ],
                    cwd=ansible_dir,
                    env=env,
                    step_name="Ansible Configuration (2/2)",
                    step_message="Running Ansible cluster deployment playbook...",
                )

            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.SUCCEEDED,
                    current_step="Completed",
                    message=f"Cluster '{request.cluster_name}' provisioned and configured successfully.",
                    completed=True,
                )
                self.op_mgr.append_log(
                    operation_id,
                    f"[{datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%SZ')}] "
                    f"Cluster build completed successfully.",
                )

        except (
            RuntimeError,
            OSError,
            ValueError,
            subprocess.SubprocessError,
            TimeoutError,
            KeyError,
        ) as e:
            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                err_msg = str(e)
                logger.error("Cluster create failed for %s: %s", operation_id, err_msg)
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.FAILED,
                    current_step="Failed",
                    message=f"Cluster provisioning failed: {err_msg}",
                    error=err_msg,
                    completed=True,
                )

    async def run_cluster_delete(
        self, request: ClusterDeleteRequest, operation_id: str
    ) -> None:
        """Execute cluster teardown: Ansible cleanup -> Terraform destroy."""
        settings = get_settings()
        repo_root = settings.repo_root

        try:
            if self._is_mock_enabled():
                await self._mock_step(
                    operation_id,
                    "Ansible Cleanup (1/2)",
                    "Resetting cluster nodes and unregistering GKE hub membership...",
                    [
                        "PLAY [Clean up GEM Cluster Nodes] **********************************",
                        "TASK [cleanup : Reset Anthos Bare Metal cluster] *******************",
                        "TASK [cleanup : Remove VXLAN overlay interfaces] *******************",
                        "PLAY RECAP **********************************************************",
                    ],
                )
                await self._mock_step(
                    operation_id,
                    "Terraform Destruction (2/2)",
                    "Destroying cluster node VMs and cloud resources...",
                    [
                        "google_compute_instance.cluster_nodes[0]: Destroying...",
                        "google_compute_instance.cluster_nodes[1]: Destroying...",
                        "google_compute_instance.cluster_nodes[2]: Destroying...",
                        "Destroy complete! Resources: 6 destroyed.",
                    ],
                )
            else:
                tf_dir = repo_root / "terraform" / "cluster"
                ansible_dir = repo_root / "ansible"

                project_id = (
                    _clean_str(request.project_id) or settings.default_project_id
                )
                zone, region = _resolve_zone_and_region(
                    request.zone,
                    request.region,
                    settings.default_zone,
                    settings.default_region,
                )
                provisioning_sa = _clean_str(
                    request.provisioning_sa_email
                ) or settings.get_provisioning_sa(project_id)
                bucket = _clean_str(
                    request.tf_state_bucket
                ) or settings.get_tf_state_bucket(project_id)

                env = os.environ.copy()
                env["CLUSTER_NAME"] = request.cluster_name
                env["PROJECT_ID"] = project_id
                env["GEM_GCP_ZONE"] = zone
                env["TF_VAR_project_id"] = project_id
                env["TF_VAR_zone"] = zone
                env["TF_VAR_region"] = region
                env["TF_VAR_cluster_name"] = request.cluster_name
                if provisioning_sa:
                    env["TF_VAR_provisioning_sa_email"] = provisioning_sa
                    env["GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"] = provisioning_sa

                # Step 1: Ansible Cleanup Playbook
                extra_vars = {
                    "cluster_name": request.cluster_name,
                    "project_id": project_id,
                    "zone": zone,
                    "region": region,
                }
                await self._execute_command(
                    operation_id=operation_id,
                    cmd=[
                        "ansible-playbook",
                        "-i",
                        "inventory.sh",
                        "cleanup.yaml",
                        "-e",
                        json.dumps(extra_vars),
                    ],
                    cwd=ansible_dir,
                    env=env,
                    step_name="Ansible Cleanup (1/2)",
                    step_message="Executing Ansible cluster cleanup playbook...",
                )

                # Step 2: Terraform Destroy
                init_cmd = [
                    "terraform",
                    "init",
                    "-input=false",
                    f"-backend-config=bucket={bucket}",
                    f"-backend-config=prefix=clusters/{request.cluster_name}/state",
                ]
                if provisioning_sa:
                    init_cmd.append(
                        f"-backend-config=impersonate_service_account={provisioning_sa}"
                    )

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=init_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Init (2/2)",
                    step_message="Initializing Terraform backend...",
                )

                destroy_cmd = [
                    "terraform",
                    "destroy",
                    "-input=false",
                    "-auto-approve",
                    f"-var=project_id={project_id}",
                    f"-var=cluster_name={request.cluster_name}",
                    f"-var=zone={zone}",
                    f"-var=region={region}",
                ]
                if provisioning_sa:
                    destroy_cmd.append(f"-var=provisioning_sa_email={provisioning_sa}")

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=destroy_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Destruction (2/2)",
                    step_message="Destroying compute instances and network bindings...",
                )

            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.SUCCEEDED,
                    current_step="Completed",
                    message=f"Cluster '{request.cluster_name}' successfully torn down.",
                    completed=True,
                )

        except (
            RuntimeError,
            OSError,
            ValueError,
            subprocess.SubprocessError,
            TimeoutError,
            KeyError,
        ) as e:
            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                err_msg = str(e)
                logger.error("Cluster delete failed for %s: %s", operation_id, err_msg)
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.FAILED,
                    current_step="Failed",
                    message=f"Cluster teardown failed: {err_msg}",
                    error=err_msg,
                    completed=True,
                )

    # Workstation Create / Delete Pipelines

    async def run_workstation_create(
        self, request: WorkstationCreateRequest, operation_id: str
    ) -> None:
        """Build Admin Workstation: Terraform apply -> Ansible admin-workstation."""
        settings = get_settings()
        repo_root = settings.repo_root

        try:
            if self._is_mock_enabled():
                await self._mock_step(
                    operation_id,
                    "Terraform Provisioning (1/2)",
                    "Provisioning admin workstation compute instance...",
                    [
                        "google_compute_instance.admin_workstation: Creating...",
                        "google_compute_instance.admin_workstation: Creation complete after 20s",
                    ],
                )
                await self._mock_step(
                    operation_id,
                    "Ansible Configuration (2/2)",
                    "Installing bmctl, Docker, and admin tools...",
                    [
                        "PLAY [Configure GEM Admin Workstation] *****************************",
                        "TASK [workstation : Install bmctl binaries] ************************",
                        "PLAY RECAP **********************************************************",
                    ],
                )
            else:
                tf_dir = repo_root / "terraform" / "admin-workstation"
                ansible_dir = repo_root / "ansible"

                project_id = (
                    _clean_str(request.project_id) or settings.default_project_id
                )
                zone, region = _resolve_zone_and_region(
                    request.zone,
                    request.region,
                    settings.default_zone,
                    settings.default_region,
                )
                provisioning_sa = _clean_str(
                    request.provisioning_sa_email
                ) or settings.get_provisioning_sa(project_id)
                bucket = settings.get_tf_state_bucket(project_id)

                env = os.environ.copy()
                env["PROJECT_ID"] = project_id
                env["GEM_GCP_ZONE"] = zone
                env["TF_VAR_project_id"] = project_id
                env["TF_VAR_zone"] = zone
                env["TF_VAR_region"] = region
                env["TF_VAR_gce_network"] = request.gce_network
                env["TF_VAR_gce_subnetwork"] = request.gce_subnetwork
                if provisioning_sa:
                    env["TF_VAR_provisioning_sa_email"] = provisioning_sa
                    env["GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"] = provisioning_sa

                init_cmd = [
                    "terraform",
                    "init",
                    "-input=false",
                    f"-backend-config=bucket={bucket}",
                    "-backend-config=prefix=admin-workstation/state",
                ]
                if provisioning_sa:
                    init_cmd.append(
                        f"-backend-config=impersonate_service_account={provisioning_sa}"
                    )

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=init_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Init (1/2)",
                    step_message="Initializing admin workstation Terraform...",
                )

                apply_cmd = [
                    "terraform",
                    "apply",
                    "-input=false",
                    "-auto-approve",
                    f"-var=project_id={project_id}",
                    f"-var=zone={zone}",
                    f"-var=region={region}",
                    f"-var=gce_network={request.gce_network}",
                    f"-var=gce_subnetwork={request.gce_subnetwork}",
                ]
                if provisioning_sa:
                    apply_cmd.append(f"-var=provisioning_sa_email={provisioning_sa}")

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=apply_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Provisioning (1/2)",
                    step_message="Provisioning admin workstation VM...",
                )

                extra_vars = {
                    "project_id": project_id,
                    "zone": zone,
                    "region": region,
                }
                await self._execute_command(
                    operation_id=operation_id,
                    cmd=[
                        "ansible-playbook",
                        "-i",
                        "inventory.sh",
                        "admin-workstation.yaml",
                        "-e",
                        json.dumps(extra_vars),
                    ],
                    cwd=ansible_dir,
                    env=env,
                    step_name="Ansible Configuration (2/2)",
                    step_message="Configuring admin workstation tools and binaries...",
                )

            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.SUCCEEDED,
                    current_step="Completed",
                    message="Admin workstation provisioned and configured successfully.",
                    completed=True,
                )

        except (
            RuntimeError,
            OSError,
            ValueError,
            subprocess.SubprocessError,
            TimeoutError,
            KeyError,
        ) as e:
            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                err_msg = str(e)
                logger.error("Workstation create failed: %s", err_msg)
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.FAILED,
                    current_step="Failed",
                    message=f"Workstation build failed: {err_msg}",
                    error=err_msg,
                    completed=True,
                )

    async def run_workstation_delete(
        self, request: WorkstationDeleteRequest, operation_id: str
    ) -> None:
        """Tear down Admin Workstation: Terraform destroy."""
        settings = get_settings()
        repo_root = settings.repo_root

        try:
            if self._is_mock_enabled():
                await self._mock_step(
                    operation_id,
                    "Terraform Destruction (1/1)",
                    "Destroying admin workstation VM...",
                    [
                        "google_compute_instance.admin_workstation: Destroying...",
                        "Destroy complete! Resources: 2 destroyed.",
                    ],
                )
            else:
                tf_dir = repo_root / "terraform" / "admin-workstation"

                project_id = (
                    _clean_str(request.project_id) or settings.default_project_id
                )
                zone, region = _resolve_zone_and_region(
                    request.zone,
                    request.region,
                    settings.default_zone,
                    settings.default_region,
                )
                provisioning_sa = _clean_str(
                    request.provisioning_sa_email
                ) or settings.get_provisioning_sa(project_id)
                bucket = _clean_str(
                    request.tf_state_bucket
                ) or settings.get_tf_state_bucket(project_id)

                env = os.environ.copy()
                env["PROJECT_ID"] = project_id
                env["GEM_GCP_ZONE"] = zone
                env["TF_VAR_project_id"] = project_id
                env["TF_VAR_zone"] = zone
                env["TF_VAR_region"] = region
                if provisioning_sa:
                    env["TF_VAR_provisioning_sa_email"] = provisioning_sa
                    env["GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"] = provisioning_sa

                init_cmd = [
                    "terraform",
                    "init",
                    "-input=false",
                    f"-backend-config=bucket={bucket}",
                    "-backend-config=prefix=admin-workstation/state",
                ]
                if provisioning_sa:
                    init_cmd.append(
                        f"-backend-config=impersonate_service_account={provisioning_sa}"
                    )

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=init_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Init (1/1)",
                    step_message="Initializing Terraform backend...",
                )

                destroy_cmd = [
                    "terraform",
                    "destroy",
                    "-input=false",
                    "-auto-approve",
                    f"-var=project_id={project_id}",
                    f"-var=zone={zone}",
                    f"-var=region={region}",
                ]
                if provisioning_sa:
                    destroy_cmd.append(f"-var=provisioning_sa_email={provisioning_sa}")

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=destroy_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Destruction (1/1)",
                    step_message="Destroying admin workstation resources...",
                )

            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.SUCCEEDED,
                    current_step="Completed",
                    message="Admin workstation destroyed successfully.",
                    completed=True,
                )

        except (
            RuntimeError,
            OSError,
            ValueError,
            subprocess.SubprocessError,
            TimeoutError,
            KeyError,
        ) as e:
            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                err_msg = str(e)
                logger.error("Workstation delete failed: %s", err_msg)
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.FAILED,
                    current_step="Failed",
                    message=f"Workstation teardown failed: {err_msg}",
                    error=err_msg,
                    completed=True,
                )

    # Edge Router Create / Delete Pipelines

    async def run_edge_router_create(
        self, request: EdgeRouterCreateRequest, operation_id: str
    ) -> None:
        """Build Edge Router: Terraform apply -> Ansible edge-router."""
        settings = get_settings()
        repo_root = settings.repo_root

        try:
            if self._is_mock_enabled():
                await self._mock_step(
                    operation_id,
                    "Terraform Provisioning (1/2)",
                    "Provisioning edge router compute instance...",
                    [
                        "google_compute_instance.edge_router: Creating...",
                        "google_compute_instance.edge_router: Creation complete after 15s",
                    ],
                )
                await self._mock_step(
                    operation_id,
                    "Ansible Configuration (2/2)",
                    "Configuring Traefik proxy and MetalLB VIP routing...",
                    [
                        "PLAY [Configure GEM Edge Router] **********************************",
                        "TASK [edge_router : Install and configure Traefik] *****************",
                        "PLAY RECAP **********************************************************",
                    ],
                )
            else:
                tf_dir = repo_root / "terraform" / "edge-router"
                ansible_dir = repo_root / "ansible"

                project_id = (
                    _clean_str(request.project_id) or settings.default_project_id
                )
                zone, region = _resolve_zone_and_region(
                    request.zone,
                    request.region,
                    settings.default_zone,
                    settings.default_region,
                )
                provisioning_sa = _clean_str(
                    request.provisioning_sa_email
                ) or settings.get_provisioning_sa(project_id)
                bucket = settings.get_tf_state_bucket(project_id)

                env = os.environ.copy()
                env["PROJECT_ID"] = project_id
                env["GEM_GCP_ZONE"] = zone
                env["TF_VAR_project_id"] = project_id
                env["TF_VAR_zone"] = zone
                env["TF_VAR_region"] = region
                env["TF_VAR_edge_router_name"] = request.edge_router_name
                env["TF_VAR_machine_type"] = request.machine_type
                env["TF_VAR_gce_network"] = request.gce_network
                env["TF_VAR_gce_subnetwork"] = request.gce_subnetwork
                if provisioning_sa:
                    env["TF_VAR_provisioning_sa_email"] = provisioning_sa
                    env["GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"] = provisioning_sa

                init_cmd = [
                    "terraform",
                    "init",
                    "-input=false",
                    f"-backend-config=bucket={bucket}",
                    "-backend-config=prefix=edge-router/state",
                ]
                if provisioning_sa:
                    init_cmd.append(
                        f"-backend-config=impersonate_service_account={provisioning_sa}"
                    )

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=init_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Init (1/2)",
                    step_message="Initializing edge router Terraform...",
                )

                apply_cmd = [
                    "terraform",
                    "apply",
                    "-input=false",
                    "-auto-approve",
                    f"-var=project_id={project_id}",
                    f"-var=zone={zone}",
                    f"-var=region={region}",
                    f"-var=edge_router_name={request.edge_router_name}",
                    f"-var=machine_type={request.machine_type}",
                    f"-var=gce_network={request.gce_network}",
                    f"-var=gce_subnetwork={request.gce_subnetwork}",
                ]
                if provisioning_sa:
                    apply_cmd.append(f"-var=provisioning_sa_email={provisioning_sa}")

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=apply_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Provisioning (1/2)",
                    step_message="Provisioning edge router VM...",
                )

                extra_vars = {
                    "project_id": project_id,
                    "zone": zone,
                    "region": region,
                    "edge_router_name": request.edge_router_name,
                }
                await self._execute_command(
                    operation_id=operation_id,
                    cmd=[
                        "ansible-playbook",
                        "-i",
                        "inventory.sh",
                        "edge-router.yaml",
                        "-e",
                        json.dumps(extra_vars),
                    ],
                    cwd=ansible_dir,
                    env=env,
                    step_name="Ansible Configuration (2/2)",
                    step_message="Configuring Traefik reverse proxy and VIP routing...",
                )

            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.SUCCEEDED,
                    current_step="Completed",
                    message="Edge router provisioned and configured successfully.",
                    completed=True,
                )

        except (
            RuntimeError,
            OSError,
            ValueError,
            subprocess.SubprocessError,
            TimeoutError,
            KeyError,
        ) as e:
            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                err_msg = str(e)
                logger.error("Edge router create failed: %s", err_msg)
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.FAILED,
                    current_step="Failed",
                    message=f"Edge router build failed: {err_msg}",
                    error=err_msg,
                    completed=True,
                )

    async def run_edge_router_delete(
        self, request: EdgeRouterDeleteRequest, operation_id: str
    ) -> None:
        """Tear down Edge Router: Terraform destroy."""
        settings = get_settings()
        repo_root = settings.repo_root

        try:
            if self._is_mock_enabled():
                await self._mock_step(
                    operation_id,
                    "Terraform Destruction (1/1)",
                    "Destroying edge router VM...",
                    [
                        "google_compute_instance.edge_router: Destroying...",
                        "Destroy complete! Resources: 2 destroyed.",
                    ],
                )
            else:
                tf_dir = repo_root / "terraform" / "edge-router"

                project_id = (
                    _clean_str(request.project_id) or settings.default_project_id
                )
                zone, region = _resolve_zone_and_region(
                    request.zone,
                    request.region,
                    settings.default_zone,
                    settings.default_region,
                )
                provisioning_sa = _clean_str(
                    request.provisioning_sa_email
                ) or settings.get_provisioning_sa(project_id)
                bucket = _clean_str(
                    request.tf_state_bucket
                ) or settings.get_tf_state_bucket(project_id)

                env = os.environ.copy()
                env["PROJECT_ID"] = project_id
                env["GEM_GCP_ZONE"] = zone
                env["TF_VAR_project_id"] = project_id
                env["TF_VAR_zone"] = zone
                env["TF_VAR_region"] = region
                env["TF_VAR_edge_router_name"] = request.edge_router_name
                if provisioning_sa:
                    env["TF_VAR_provisioning_sa_email"] = provisioning_sa
                    env["GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"] = provisioning_sa

                init_cmd = [
                    "terraform",
                    "init",
                    "-input=false",
                    f"-backend-config=bucket={bucket}",
                    "-backend-config=prefix=edge-router/state",
                ]
                if provisioning_sa:
                    init_cmd.append(
                        f"-backend-config=impersonate_service_account={provisioning_sa}"
                    )

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=init_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Init (1/1)",
                    step_message="Initializing Terraform backend...",
                )

                destroy_cmd = [
                    "terraform",
                    "destroy",
                    "-input=false",
                    "-auto-approve",
                    f"-var=project_id={project_id}",
                    f"-var=zone={zone}",
                    f"-var=region={region}",
                    f"-var=edge_router_name={request.edge_router_name}",
                ]
                if provisioning_sa:
                    destroy_cmd.append(f"-var=provisioning_sa_email={provisioning_sa}")

                await self._execute_command(
                    operation_id=operation_id,
                    cmd=destroy_cmd,
                    cwd=tf_dir,
                    env=env,
                    step_name="Terraform Destruction (1/1)",
                    step_message="Destroying edge router resources...",
                )

            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.SUCCEEDED,
                    current_step="Completed",
                    message="Edge router destroyed successfully.",
                    completed=True,
                )

        except (
            RuntimeError,
            OSError,
            ValueError,
            subprocess.SubprocessError,
            TimeoutError,
            KeyError,
        ) as e:
            record = self.op_mgr._operations.get(operation_id)
            if record and record.status != OperationStatus.CANCELLED:
                err_msg = str(e)
                logger.error("Edge router delete failed: %s", err_msg)
                await self.op_mgr.update_operation(
                    operation_id=operation_id,
                    status=OperationStatus.FAILED,
                    current_step="Failed",
                    message=f"Edge router teardown failed: {err_msg}",
                    error=err_msg,
                    completed=True,
                )


_runner_instance = ProcessRunner()


def get_process_runner() -> ProcessRunner:
    """Return singleton ProcessRunner instance."""
    return _runner_instance
