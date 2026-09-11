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

import logging

from fastapi import APIRouter, Depends, Query, status

from gem_api.models.configsync import RootSyncListResponse
from gem_api.models.networks import SecondaryNetworkListResponse
from gem_api.models.pods import (
    PodActionResponse,
    PodCreateRequest,
    PodItem,
    PodListResponse,
)
from gem_api.models.status import ClusterStatusResponse
from gem_api.models.vms import (
    GenericActionResponse,
    VirtualMachineDeployRequest,
    VirtualMachineItem,
    VirtualMachineListResponse,
    VirtualMachinePowerRequest,
    VirtualMachinePowerResponse,
)
from gem_api.services.k8s_client import K8sService, get_k8s_service

logger = logging.getLogger("gem_api.routers.cluster_operations")

router = APIRouter(prefix="/clusters", tags=["Workloads & Telemetry"])


# Cluster Status & Live Metrics
@router.get(
    "/{cluster_name}/status",
    response_model=ClusterStatusResponse,
    status_code=status.HTTP_200_OK,
    summary="Get cluster health and live metrics",
    description="Returns node status, resource allocation, and overall health for a deployed cluster.",
)
async def get_cluster_status(
    cluster_name: str,
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID. Defaults to active project.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> ClusterStatusResponse:
    """Get cluster status and live metrics."""
    return await k8s_service.get_cluster_status(
        cluster_name=cluster_name, project_id=project_id
    )


# Secondary Networks
@router.get(
    "/{cluster_name}/networks",
    response_model=SecondaryNetworkListResponse,
    status_code=status.HTTP_200_OK,
    summary="List secondary networks",
    description="Returns secondary VLAN network attachments configured on the cluster.",
)
async def list_secondary_networks(
    cluster_name: str,
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID. Defaults to active project.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> SecondaryNetworkListResponse:
    """List secondary networks."""
    return await k8s_service.list_secondary_networks(
        cluster_name=cluster_name, project_id=project_id
    )


# Virtual Machines (KubeVirt)
@router.get(
    "/{cluster_name}/vms",
    response_model=VirtualMachineListResponse,
    status_code=status.HTTP_200_OK,
    summary="List Virtual Machines",
    description="Returns KubeVirt VirtualMachines deployed on the cluster.",
)
async def list_vms(
    cluster_name: str,
    namespace: str | None = Query(
        default=None,
        description="Filter by Kubernetes namespace. Defaults to all namespaces.",
    ),
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> VirtualMachineListResponse:
    """List virtual machines in cluster."""
    return await k8s_service.list_vms(
        cluster_name=cluster_name,
        namespace=namespace,
        project_id=project_id,
    )


@router.post(
    "/{cluster_name}/vms",
    response_model=VirtualMachineItem,
    status_code=status.HTTP_201_CREATED,
    summary="Deploy a new Virtual Machine",
    description="Deploys a new KubeVirt VirtualMachine on the target GEM cluster.",
)
async def deploy_vm(
    cluster_name: str,
    request: VirtualMachineDeployRequest,
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> VirtualMachineItem:
    """Deploy a virtual machine."""
    return await k8s_service.deploy_vm(
        cluster_name=cluster_name,
        request=request,
        project_id=project_id,
    )


@router.post(
    "/{cluster_name}/vms/{vm_name}/power",
    response_model=VirtualMachinePowerResponse,
    status_code=status.HTTP_200_OK,
    summary="Toggle Virtual Machine power state",
    description="Starts or stops a KubeVirt VirtualMachine.",
)
async def power_vm(
    cluster_name: str,
    vm_name: str,
    request: VirtualMachinePowerRequest,
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> VirtualMachinePowerResponse:
    """Power on or off a virtual machine."""
    return await k8s_service.power_vm(
        cluster_name=cluster_name,
        vm_name=vm_name,
        running=request.running,
        namespace=request.namespace,
        project_id=project_id,
    )


@router.delete(
    "/{cluster_name}/vms/{vm_name}",
    response_model=GenericActionResponse,
    status_code=status.HTTP_200_OK,
    summary="Delete a Virtual Machine",
    description="Deletes a KubeVirt VirtualMachine from the cluster.",
)
async def delete_vm(
    cluster_name: str,
    vm_name: str,
    namespace: str = Query(
        default="default",
        description="Kubernetes namespace where the VM resides.",
    ),
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> GenericActionResponse:
    """Delete a virtual machine."""
    return await k8s_service.delete_vm(
        cluster_name=cluster_name,
        vm_name=vm_name,
        namespace=namespace,
        project_id=project_id,
    )


# ConfigSync RootSyncs
@router.get(
    "/{cluster_name}/configsync",
    response_model=RootSyncListResponse,
    status_code=status.HTTP_200_OK,
    summary="List ConfigSync RootSyncs",
    description="Returns GitOps RootSync synchronization statuses for the cluster.",
)
async def list_rootsyncs(
    cluster_name: str,
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> RootSyncListResponse:
    """List RootSync resources."""
    return await k8s_service.list_rootsyncs(
        cluster_name=cluster_name, project_id=project_id
    )


# Pod Management
@router.get(
    "/{cluster_name}/pods",
    response_model=PodListResponse,
    status_code=status.HTTP_200_OK,
    summary="List Pods",
    description="Returns Kubernetes Pods running in the cluster.",
)
async def list_pods(
    cluster_name: str,
    namespace: str | None = Query(
        default=None,
        description="Filter by Kubernetes namespace. Defaults to all namespaces.",
    ),
    label_selector: str | None = Query(
        default=None,
        description="Kubernetes label selector query (e.g. app=nginx).",
    ),
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> PodListResponse:
    """List pods in cluster."""
    return await k8s_service.list_pods(
        cluster_name=cluster_name,
        namespace=namespace,
        label_selector=label_selector,
        project_id=project_id,
    )


@router.post(
    "/{cluster_name}/pods",
    response_model=PodItem,
    status_code=status.HTTP_201_CREATED,
    summary="Create a Pod",
    description="Deploys a new Pod on the target cluster.",
)
async def create_pod(
    cluster_name: str,
    request: PodCreateRequest,
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> PodItem:
    """Create a pod."""
    return await k8s_service.create_pod(
        cluster_name=cluster_name,
        request=request,
        project_id=project_id,
    )


@router.delete(
    "/{cluster_name}/pods/{pod_name}",
    response_model=PodActionResponse,
    status_code=status.HTTP_200_OK,
    summary="Delete a Pod",
    description="Deletes a Pod from the cluster.",
)
async def delete_pod(
    cluster_name: str,
    pod_name: str,
    namespace: str = Query(
        default="default",
        description="Kubernetes namespace where the Pod resides.",
    ),
    grace_period_seconds: int | None = Query(
        default=None,
        ge=0,
        description="Deletion grace period in seconds.",
    ),
    project_id: str | None = Query(
        default=None,
        description="Target GCP Project ID.",
    ),
    k8s_service: K8sService = Depends(get_k8s_service),
) -> PodActionResponse:
    """Delete a pod."""
    return await k8s_service.delete_pod(
        cluster_name=cluster_name,
        pod_name=pod_name,
        namespace=namespace,
        grace_period_seconds=grace_period_seconds,
        project_id=project_id,
    )
