# GEM REST API Specification

This document details the requirements and specifications for the GEM REST API built with FastAPI.

## 1. Core Infrastructure and Lifecycle Endpoints

### 1.1 Build a new GEM cluster
* **Path:** `POST /api/v1/clusters/create`
* **Response:** `202 Accepted` with the `operation_id` (the `cluster_name` is used as the unique operation ID).
* **Workflow:**
  1. Validates the request payload.
  2. Runs Terraform build scripts (`terraform/cluster`) to provision infrastructure.
  3. Invokes Ansible playbooks (`ansible/create-cluster.yaml`) to configure the cluster.
  4. Returns succinct, human-readable text for intermediate and final status.
* **Available Request Parameters and Defaults:**
  * `cluster_name` (*string*, default: `"gem-cluster-1"`): Name of the cluster. **Constraints:** Max 26 characters; `len(cluster_name) + len(zone) + len(project_id) + 15 <= 63` to prevent Kubernetes node FQDN label limit errors.
  * `project_id` (*string*, default: resolved from active GCP environment / `gcloud config`): Target GCP Project ID.
  * `zone` (*string*, default: `"us-central1-a"` or `GEM_GCP_ZONE` env): Target GCP Zone (e.g. `us-central1-a`).
  * `region` (*string*, default: derived from zone prefix, e.g. `us-central1`): Target GCP Region.
  * `hardware_variant` (*string*, default: `"g2-small-64gb"`): Target GDC hardware offering to emulate.
    * Supported options: `g2-small-64gb` (default, 32 vCPU, 64 GB RAM, 3.84 TB Hyperdisk), `g2-small-128gb` (32 vCPU, 128 GB RAM, 3.84 TB Hyperdisk), `g2-medium` (48 vCPU, 128 GB RAM, 3.84 TB Hyperdisk), `g2-large` (64 vCPU, 128 GB RAM, 3.84 TB Hyperdisk), `g1-medium` (32 vCPU, 64 GB RAM, 1.6 TB SSD), `g1-large` (64 vCPU, 128 GB RAM, 3.2 TB SSD), `dev-and-test` (8 vCPU, 32 GB RAM, 150 GB SSD).
  * `emulate_gdc_version` (*string*, default: `"1.13.0"`): GDC Connected version to emulate. Maps to specific Anthos Bare Metal (`bmctl`) and gVisor versions.
    * Supported options: `1.13.0` (ABM `1.34.100-gke.97`, gVisor `20241021.0`), `1.12.1` (ABM `1.33.300-gke.60`), `1.12.0` (ABM `1.33.300-gke.60`), `1.11.1` (ABM `1.32.700-gke.64`).
  * `provisioning_sa_email` (*string*, default: `"tf-provisioner@<project_id>.iam.gserviceaccount.com"`): GCP SA impersonated by Terraform for cloud resource management.
  * `gcp_cluster_admin_sa` (*string*, default: `"gem-cluster-admin@<project_id>.iam.gserviceaccount.com"`): GCP SA granted cluster-admin access via GKE Connect Gateway.
  * `gce_network` (*string*, default: `"gem-clusters-vpc"`): VPC network name.
  * `gce_subnetwork` (*string*, default: `"gem-clusters-subnet"`): Subnet name.
  * `node_storage_size` (*string*, default: `"100GB"`): Size of the node local storage partition before TopoLVM volume group allocation.
  * `pod_cidr_blocks` (*string*, default: `"10.0.0.0/17"`): CIDR block for primary pod networking.
  * `services_cidr_blocks` (*string*, default: `"10.96.0.0/12"`): CIDR block for Kubernetes cluster services.
  * `max_pods_per_node` (*integer*, default: `250`): Maximum pods schedulable per node.
  * `secondary_networks` (*array of objects*, optional / default: standard VLANs from `all.yaml`): Definitions for Multus secondary networks (each with `name`, `vlan_id`, `subnet`, `gateway`, `vip_pool`, `pod_cidr`, `per_node_ipam_size`).

### 1.2 Tear down a previously-built GEM cluster
* **Path:** `POST /api/v1/clusters/delete`
* **Response:** `202 Accepted` with the `operation_id` (the `cluster_name` is used as the unique operation ID).
* **Workflow:**
  1. Validates the request payload.
  2. Invokes Ansible playbooks (`ansible/cleanup.yaml`) to reset `bmctl` and unregister the cluster from GKE Hub.
  3. Runs Terraform destroy scripts (`terraform/cluster`) to destroy the VM infrastructure.
  4. Returns succinct, human-readable text for intermediate and final status.
* **Request Parameters and Defaults:**
  * `cluster_name` (*string*, **required**): Exact identifier of the cluster to be torn down.
  * `project_id` (*string*, default: resolved from active GCP environment / `gcloud config`): GCP Project ID hosting the cluster.
  * `zone` (*string*, default: `"us-central1-a"` or `GEM_GCP_ZONE` env): GCP Zone where cluster node VMs reside.
  * `region` (*string*, default: derived from zone prefix, e.g. `us-central1`): GCP Region.
  * `provisioning_sa_email` (*string*, default: `"tf-provisioner@<project_id>.iam.gserviceaccount.com"`): Email of the Terraform provisioning SA to impersonate.
  * `tf_state_bucket` (*string*, default: `"gem-<project_id>-tfstate"`): GCS bucket holding remote state.

### 1.3 Build a new GEM Admin Workstation
* **Path:** `POST /api/v1/workstation/create`
* **Response:** `202 Accepted` with `operation_id` (e.g. `"gem-admin-ws"`).
* **Workflow:**
  1. Validates the request payload.
  2. Runs Terraform build scripts (`terraform/admin-workstation`).
  3. Invokes Ansible playbooks (`ansible/admin-workstation.yaml`).
  4. Returns succinct, human-readable text for intermediate and final status.
* **Available Request Parameters and Defaults:**
  * `project_id` (*string*, default: resolved from active GCP environment): Target GCP Project ID.
  * `zone` (*string*, default: `"us-central1-a"` or `GEM_GCP_ZONE` env): Target GCP Zone.
  * `region` (*string*, default: derived from zone prefix, e.g. `us-central1`): Target GCP Region.
  * `provisioning_sa_email` (*string*, default: `"tf-provisioner@<project_id>.iam.gserviceaccount.com"`): SA impersonated for Terraform provisioning.
  * `gce_network` (*string*, default: `"gem-clusters-vpc"`): VPC network name.
  * `gce_subnetwork` (*string*, default: `"gem-clusters-subnet"`): Subnet name.

### 1.4 Tear down a GEM Admin Workstation
* **Path:** `POST /api/v1/workstation/delete`
* **Response:** `202 Accepted` with `operation_id` (`"gem-admin-ws"`).
* **Workflow:**
  1. Validates the request payload.
  2. Runs Terraform destroy scripts (`terraform/admin-workstation`).
  3. Returns succinct, human-readable text for intermediate and final status.
* **Request Parameters and Defaults:**
  * `project_id` (*string*, default: resolved from active GCP environment / `gcloud config`): Target GCP Project ID.
  * `zone` (*string*, default: `"us-central1-a"` or `GEM_GCP_ZONE` env): Target GCP Zone.
  * `region` (*string*, default: derived from zone prefix, e.g. `us-central1`): Target GCP Region.
  * `provisioning_sa_email` (*string*, default: `"tf-provisioner@<project_id>.iam.gserviceaccount.com"`): Email of the Terraform provisioning SA to impersonate.
  * `tf_state_bucket` (*string*, default: `"gem-<project_id>-tfstate"`): GCS bucket holding remote state.

### 1.5 Build a new GEM Edge Router
* **Path:** `POST /api/v1/edge-router/create`
* **Response:** `202 Accepted` with `operation_id` (e.g. `"gem-edge-router"`).
* **Workflow:**
  1. Validates the request payload.
  2. Runs Terraform build scripts (`terraform/edge-router`).
  3. Invokes Ansible playbooks (`ansible/edge-router.yaml`).
  4. Returns succinct, human-readable text for intermediate and final status.
* **Available Request Parameters and Defaults:**
  * `project_id` (*string*, default: resolved from active GCP environment): Target GCP Project ID.
  * `zone` (*string*, default: `"us-central1-a"` or `GEM_GCP_ZONE` env): Target GCP Zone.
  * `region` (*string*, default: derived from zone prefix, e.g. `us-central1`): Target GCP Region.
  * `provisioning_sa_email` (*string*, default: `"tf-provisioner@<project_id>.iam.gserviceaccount.com"`): SA impersonated for Terraform provisioning.
  * `edge_router_name` (*string*, default: `"gem-edge-router"`): GCE instance name.
  * `machine_type` (*string*, default: `"e2-small"`): GCE VM machine type.
  * `gce_network` (*string*, default: `"gem-clusters-vpc"`): VPC network name.
  * `gce_subnetwork` (*string*, default: `"gem-clusters-subnet"`): Subnet name.

### 1.6 Tear down a GEM Edge Router
* **Path:** `POST /api/v1/edge-router/delete`
* **Response:** `202 Accepted` with `operation_id` (`"gem-edge-router"`).
* **Workflow:**
  1. Validates the request payload.
  2. Runs Terraform destroy scripts (`terraform/edge-router`).
  3. Returns succinct, human-readable text for intermediate and final status.
* **Request Parameters and Defaults:**
  * `project_id` (*string*, default: resolved from active GCP environment / `gcloud config`): Target GCP Project ID.
  * `zone` (*string*, default: `"us-central1-a"` or `GEM_GCP_ZONE` env): Target GCP Zone.
  * `region` (*string*, default: derived from zone prefix, e.g. `us-central1`): Target GCP Region.
  * `provisioning_sa_email` (*string*, default: `"tf-provisioner@<project_id>.iam.gserviceaccount.com"`): Email of the Terraform provisioning SA to impersonate.
  * `edge_router_name` (*string*, default: `"gem-edge-router"`): GCE instance name.
  * `tf_state_bucket` (*string*, default: `"gem-<project_id>-tfstate"`): GCS bucket holding remote state.

### 1.7 List all deployed GEM clusters
* **Path:** `GET /api/v1/clusters`
* **Query Parameters:**
  * `project_id` (*string*, optional): Filter by GCP Project ID.
* **Response:** JSON-formatted array of all deployed GEM clusters, including attributes corresponding to `gcloud container clusters list` (e.g. `name`, `location`, `master_version`, `status`, `node_count`, `endpoint`, etc.).

### 1.8 List GCP Projects
* **Path:** `GET /api/v1/projects`
* **Query Parameters:**
  * `limit` (*integer*, optional, default: `50`): Maximum number of projects to return.
* **Response:**
  ```json
  {
    "projects": [
      { "project_id": "gem-dev-project", "name": "GEM Development Project" },
      { "project_id": "gem-prod-project", "name": "GEM Production Project" }
    ]
  }
  ```

## 2. Operations and Observability Endpoints

### 2.1 Query Operation Status
* **Path:** `GET /api/v1/operations/{operation_id}`
* **Response:**
  ```json
  {
    "operation_id": "gem-cluster-1",
    "operation_type": "CLUSTER_CREATE",
    "status": "RUNNING",
    "target_resource": "gem-cluster-1",
    "current_step": "Ansible Configuration (2/2)",
    "message": "Configuring TopoLVM storage provider and storage classes...",
    "created_at": "2026-08-21T10:15:00Z",
    "updated_at": "2026-08-21T10:28:30Z",
    "completed_at": null,
    "error": null
  }
  ```

### 2.2 Query Operation Logs
* **Path:** `GET /api/v1/operations/{operation_id}/logs`
* **Query Parameters:**
  * `tail` (*integer*, optional): Number of latest log lines to return (e.g. `tail=100`).
  * `stream` (*boolean*, optional, default `false`): When set to `true`, streams log lines live using Server-Sent Events (`text/event-stream`).
* **Response (JSON mode):**
  ```json
  {
    "operation_id": "gem-cluster-1",
    "status": "RUNNING",
    "log_lines": [
      "[2026-08-21T10:15:05Z] Initializing Terraform cluster module...",
      "[2026-08-21T10:15:20Z] Applying Terraform resources...",
      "[2026-08-21T10:18:40Z] Terraform completed successfully. Starting Ansible playbook create-cluster.yaml..."
    ]
  }
  ```

### 2.3 Cancel / Abort Operation
* **Path:** `POST /api/v1/operations/{operation_id}/cancel`
* **Workflow:** Terminates the underlying active Terraform or Ansible process group (`SIGTERM` $\rightarrow$ `SIGKILL`), cleans up transient subprocess locks, and marks the operation status as `CANCELLED`.
* **Response:**
  ```json
  {
    "success": true,
    "operation_id": "gem-cluster-1",
    "status": "CANCELLED",
    "message": "Operation 'gem-cluster-1' was cancelled and background processes terminated."
  }
  ```

## 3. Cluster and Workload Management Endpoints

### 3.1 Cluster Health and Live Metrics
* **Path:** `GET /api/v1/clusters/{cluster_name}/status`
* **Query Parameters:**
  * `project_id` (*string*, optional): Target GCP Project ID.
* **Response:**
  ```json
  {
    "connected": true,
    "cluster_name": "gem-cluster-1",
    "mode": "Live Connected",
    "nodes": [
      {
        "name": "node1",
        "status": "Ready",
        "role": "Control Plane",
        "ip": "10.200.54.2",
        "cpu_usage": "320m",
        "cpu_percent": 4,
        "mem_usage": "3100Mi",
        "mem_percent": 5
      }
    ],
    "metrics": {
      "total_cpu": "96 vCPU",
      "used_cpu": "18 vCPU",
      "total_mem": "192 GB",
      "used_mem": "42 GB",
      "storage_allocated": "850 GB / 3.9 TB"
    }
  }
  ```

### 3.2 Secondary Networks Management
* **Path:** `GET /api/v1/clusters/{cluster_name}/networks`
* **Query Parameters:**
  * `project_id` (*string*, optional): Target GCP Project ID.
* **Workflow:** Queries the Kubernetes API for `networking.gke.io/v1 Network` Custom Resources deployed on the target cluster.
* **Response:**
  ```json
  {
    "networks": [
      {
        "name": "vlan-123",
        "vlan_id": 123,
        "subnet": "172.16.12.0/24",
        "gateway": "172.16.12.1",
        "vip_pool": "172.16.12.200-172.16.12.250",
        "purpose": "Secondary VLAN Overlay",
        "interface_name": "gdcenet0.123",
        "status": "Active"
      }
    ]
  }
  ```

### 3.3 Virtual Machine Management (VMRuntime)

#### List Virtual Machines
* **Path:** `GET /api/v1/clusters/{cluster_name}/vms`
* **Query Parameters:**
  * `project_id` (*string*, optional): Target GCP Project ID.
  * `namespace` (*string*, optional, default: all namespaces): Filter by Kubernetes namespace.
* **Workflow:** Queries the Kubernetes API for `kubevirt.io/v1 VirtualMachine` resources.
* **Response:**
  ```json
  {
    "vms": [
      {
        "name": "ubuntu-edge-server-01",
        "namespace": "default",
        "status": "Running",
        "cpus": 4,
        "memory": "8Gi",
        "ip": "10.240.1.50",
        "image": "ubuntu-22.04-server-cloudimg-amd64",
        "uptime": "18h 32m",
        "power_state": "Running"
      }
    ]
  }
  ```

#### Deploy Virtual Machine
* **Path:** `POST /api/v1/clusters/{cluster_name}/vms`
* **Request Body:**
  * `name` (*string*, **required**): Name of the virtual machine.
  * `namespace` (*string*, default: `"default"`): Kubernetes namespace.
  * `cpus` (*integer*, default: `2`): Number of vCPU cores.
  * `memory` (*string*, default: `"4Gi"`): Memory allocation (e.g. `"4Gi"`, `"8Gi"`).
  * `image` (*string*, **required**): Disk image name or URL.
  * `image_type` (*string*, default: `"preset"`): `"preset"` (containerDisk: Ubuntu, Debian, RHEL, CentOS) or `"custom-url"` (DataVolume HTTP source).
* **Response:** `201 Created` with the deployed VM metadata.

#### Power State Toggle (Start / Stop)
* **Path:** `POST /api/v1/clusters/{cluster_name}/vms/{vm_name}/power`
* **Request Body:**
  * `namespace` (*string*, default: `"default"`): Kubernetes namespace.
  * `running` (*boolean*, **required**): Desired power state (`true` to start, `false` to stop).
* **Workflow:** Patches `spec.running` on the target `kubevirt.io/v1 VirtualMachine` object.
* **Response:**
  ```json
  {
    "success": true,
    "vm_name": "ubuntu-edge-server-01",
    "power_state": "Running",
    "message": "VM 'ubuntu-edge-server-01' power state set to Running."
  }
  ```

#### Delete Virtual Machine
* **Path:** `DELETE /api/v1/clusters/{cluster_name}/vms/{vm_name}`
* **Query Parameters:**
  * `namespace` (*string*, default: `"default"`): Kubernetes namespace.
* **Response:**
  ```json
  {
    "success": true,
    "vm_name": "ubuntu-edge-server-01",
    "message": "VirtualMachine 'ubuntu-edge-server-01' deleted successfully."
  }
  ```

### 3.4 ConfigSync RootSync Management
* **Path:** `GET /api/v1/clusters/{cluster_name}/configsync`
* **Query Parameters:**
  * `project_id` (*string*, optional): Target GCP Project ID.
* **Workflow:** Queries `configsync.gke.io/v1beta1 RootSync` objects from the `config-management-system` namespace.
* **Response:**
  ```json
  {
    "root_syncs": [
      {
        "name": "root-sync-foundation",
        "namespace": "config-management-system",
        "repo": "https://github.com/google-cloud-platform/gdc-hybrid-manifests.git",
        "branch": "main",
        "dir": "/clusters/core-infrastructure",
        "auth": "none",
        "period": "15s",
        "status": "SYNCED",
        "commit": "4b825dc6f",
        "last_synced": "2026-08-21T11:00:00Z",
        "message": "Foundation networking, Robin SDS storage class, and ingress controllers reconciled."
      }
    ]
  }
  ```

### 3.5 Pod Management

#### List Pods
* **Path:** `GET /api/v1/clusters/{cluster_name}/pods`
* **Query Parameters:**
  * `project_id` (*string*, optional): Target GCP Project ID.
  * `namespace` (*string*, optional, default: all namespaces): Filter by Kubernetes namespace.
  * `label_selector` (*string*, optional): Kubernetes label selector query (e.g. `app=nginx`).
* **Workflow:** Queries the Kubernetes CoreV1 API for Pod resources in the cluster.
* **Response:**
  ```json
  {
    "pods": [
      {
        "name": "nginx-webserver-7f89b4c-9kl2z",
        "namespace": "default",
        "status": "Running",
        "ready": "1/1",
        "restarts": 0,
        "age": "3h 12m",
        "ip": "10.0.1.42",
        "node_name": "node1",
        "containers": [
          {
            "name": "nginx",
            "image": "nginx:alpine",
            "ready": true,
            "state": "running"
          }
        ]
      }
    ]
  }
  ```

#### Create / Deploy Pod
* **Path:** `POST /api/v1/clusters/{cluster_name}/pods`
* **Request Body:**
  * `name` (*string*, **required**): Name of the pod.
  * `namespace` (*string*, default: `"default"`): Kubernetes namespace.
  * `image` (*string*, **required**): Container image.
  * `command` (*array of strings*, optional): Container entrypoint command.
  * `port` (*integer*, optional): Container port to expose.
  * `env` (*object / key-value map*, optional): Environment variables.
  * `labels` (*object / key-value map*, optional): Pod metadata labels.
  * `annotations` (*object / key-value map*, optional): Pod annotations (e.g., `networking.gke.io/interfaces` for secondary networks or `gvisor.gke.io/runtime` for sandboxing).
  * `raw_manifest` (*string*, optional): Raw YAML/JSON string manifest for full custom Pod specifications.
* **Workflow:** Submits a `v1/Pod` definition to the Kubernetes API.
* **Response:** `201 Created` with the created pod metadata.

#### Delete Pod
* **Path:** `DELETE /api/v1/clusters/{cluster_name}/pods/{pod_name}`
* **Query Parameters:**
  * `project_id` (*string*, optional): Target GCP Project ID.
  * `namespace` (*string*, default: `"default"`): Kubernetes namespace.
  * `grace_period_seconds` (*integer*, optional): Deletion grace period in seconds.
* **Workflow:** Deletes the pod resource via the Kubernetes CoreV1 API.
* **Response:**
  ```json
  {
    "success": true,
    "pod_name": "nginx-webserver-7f89b4c-9kl2z",
    "namespace": "default",
    "message": "Pod 'nginx-webserver-7f89b4c-9kl2z' deleted successfully."
  }
  ```
