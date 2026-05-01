
<h1 align=center>GEM — GDC EMulation Environment</h1>
</p>
<p align=center>
<img src="docs/img/gem-logo.png" height="250"/>
</p>


GEM, the GDC EMulation Environment is a Google Distributed Cloud Connected (GDC) like platform running entirely on Google Compute Engine. It accurately mimics a physical GDC Connected environment using isolated virtual resources in GCP, allowing for rapid prototyping, architecture and development testing, and robust end-to-end validation of GDC workloads. GEM is **not** a [Google Distributed Cloud Connected](https://cloud.google.com/distributed-cloud-connected) variant, rather it uses a opinionated build of [Google Distributed Cloud (software only) for bare metal](https://docs.cloud.google.com/kubernetes-engine/distributed-cloud/bare-metal/docs/concepts/about-bare-metal) to accurately emulate GDC Connected Servers.

## GEM Design

The GEM platform takes a modular design approach to isolate stable, foundational infrastructure from ephemeral GEM workload clusters:

*  **Foundation (`terraform/foundation`)**: Provisions the core VPC networks (`gem-clusters-vpc`), subnets, Cloud NAT, and foundational Service Accounts required to run the environment.
*  **Admin Workstation (`terraform/admin-workstation`)**:  A dedicated, GCE instance (`gem-admin-ws`) used to build and manage the GEM workload clusters.
*  **Edge Router (`terraform/edge-router`)**: An optional ingress VM that provides stable external access to the Kubernetes Services running within the GEM cluster.
*  **GEM Clusters (`terraform/cluster`)**: A dedicated, 3-node GDC-like environment, used to run Kubernetes workloads, including virtual machines with VMRuntime. Multiple isolated GEM clusters can be deployed in the same GCP project.


## 🚀 Getting Started

### Prerequisites
*   [Google Cloud SDK](https://cloud.google.com/sdk) (`gcloud`) installed, authenticated and configured.
    * Ensure your Google Cloud SDK [Application Default Credentials](https://docs.cloud.google.com/docs/authentication/application-default-credentials) are also configured
*   [HashiCorp Terraform CLI](https://developer.hashicorp.com/terraform/install) (`terraform`) installed.
*   [Ansible](https://docs.ansible.com/projects/ansible/latest/installation_guide/intro_installation.html) (`ansible-playbook`) installed.
*  [jq](https://jqlang.org/download/) installed.
* This [GEM repo](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment) cloned to a machine used to provision a GEM environment. To get started, this can be your local workstation.

### Environment Setup
Some initial setup needs to be completed before you can begin to provision GEM clusters. These environment variables will be used throughout the GEM creation process:
```bash
export CLUSTER_NAME=gem-cluster-1
export PROJECT_ID=your-gcp-project-id
export TF_STATE_BUCKET=gem-${PROJECT_ID}-tfstate
# The local directory of this repo
export REPO_BASE=~/src/gem-gdc-emulation-environment
# A GCP Service Account used by Terraform to provision the GEM infrastructure
export PROVISIONING_SA_EMAIL="tf-provisioner@${PROJECT_ID}.iam.gserviceaccount.com"
# A GCP Service Account used by Ansible to build a GEM cluster
export IMPERSONATE_SA_EMAIL="gem-cluster-admin@${PROJECT_ID}.iam.gserviceaccount.com"
```

### Configure your GCP Project
Initial setup of a GCP project needs to be completed before you can provision GEM resources. This includes:
  * Enable initial required GCP APIs
  * Creation of a dedicated Google Cloud Storage bucket to manage Terraform state
  * Creation of a provisioner GCP Service account, which is used to create the various compute resources throughout the project.

A helper script is provided to automate this work:

```bash
# Creates the SA, grants permissions, generates backend.tf and tfvars files
cd ${REPO_BASE}
./project-setup.sh
```

### Deploy Foundation and Admin Workstation
*This should only be required once per GCP project.*

```bash

# Build and deploy the GEM foundation
cd ${REPO_BASE}/terraform/foundation

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=foundation/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply

# Deploy the Admin Workstation
cd ${REPO_BASE}/terraform/admin-workstation

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=admin-workstation/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply

# Configure the Admin Workstation
cd ${REPO_BASE}/ansible

ansible-playbook admin-workstation.yaml
```

### Provision and Deploy a GEM Cluster
You can deploy as many isolated GEM clusters as your GCP quota allows by changing the `CLUSTER_NAME`.

GEM provides emulation for the two most recent major versions of GDC. This can be specified
at build time through the `emulate_gdc_version` variable. If the `emulate_gdc_version` is not
specified, the most recent GDC version will be emulated. Available options can be found in
[ansible/group_vars/all.yaml](ansible/group_vars/all.yaml)


The cluster build process takes approximately 30 minutes to complete.

```bash
# Provision the 3 Compute Engine nodes
cd ${REPO_BASE}/terraform/cluster

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=clusters/${CLUSTER_NAME}/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply -var="cluster_name=${CLUSTER_NAME}"


cd ${REPO_BASE}/ansible

# Build a GEM cluster, emulating the latest available version
ansible-playbook create-cluster.yaml

# Or, build a GEM cluster emulating GDC version 1.11.1
ansible-playbook create-cluster.yaml --extra-vars "emulate_gdc_version=1.11.1"
```

**A Note on Virtualization:**
If your GCP Project enforces Shielded VMs (Secure Boot), the GEM cluster will seamlessly fall back to QEMU software emulation. However, this strips Hyper-V CPU features, causing GDC `VirtualMachine` objects with `osType: Windows` to fail scheduling. If you need Windows guests, you must either deploy in a project without Secure Boot (to enable hardware KVM) or temporarily set `osType: Linux` on the Windows VM manifest as a workaround.

### Deploy the GEM Edge Router (Optional)
If you require Traefik proxy access into your internal MetalLB VIPs, deploy the Edge Router.

```bash
cd ${REPO_BASE}/terraform/edge-router

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=edge-router/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply

cd ${REPO_BASE}/ansible
ansible-playbook edge-router.yaml
```

## Accessing Your Cluster

The complete cluster build process takes upwards of 30 minutes to complete, you can monitor the Ansible playbook output for basic build progress.

To watch the installation in real-time, SSH into the admin workstation:

```bash
# Connect to the admin workstation
# All cluster operations are run as the gem user
gcloud compute ssh gem@gem-admin-ws --tunnel-through-iap --project=${PROJECT_ID}

# Tail the build logs
tail -f ~/bmctl-workspace/${CLUSTER_NAME}/log/create-cluster-*/create-cluster.log
```

### Initial GEM Cluster Access
Once the build has finished, an admin-level kubeconfig is made available on the Admin Workstation under
`~/bmctl-workspace/${CLUSTER_NAME}/${CLUSTER_NAME}-kubeconfig`.

The user credentials in the admin workstation kubeconfig have admin level permissions
and is not representative of the real-world permissions a typical user will have on a GEM
cluster. This is **not** the primary means of cluster access, but should be used primarily
for break-glass admin-level troubleshooting.

```bash
kubectl get nodes --kubeconfig /home/gem/bmctl-workspace/${CLUSTER_NAME}/${CLUSTER_NAME}-kubeconfig
```

### Local Access via GKE Connect Gateway
To access the cluster remotely from your local workstation, use the GKE Connect Gateway by
impersonating the `gem-cluster-admin` service account:

```bash
export GEM_CLUSTER_ADMIN_SA_EMAIL="gem-cluster-admin@${PROJECT_ID}.iam.gserviceaccount.com"

gcloud config set auth/impersonate_service_account ${GEM_CLUSTER_ADMIN_SA_EMAIL}
gcloud container fleet memberships get-credentials ${CLUSTER_NAME}

kubectl config set-credentials "connectgateway_${PROJECT_ID}_global_${CLUSTER_NAME}" \
  --token=$(gcloud auth print-access-token)

kubectl get nodes
```

From this point forward, you have a functioning GDC-like environment, which can be configured like any other GDC cluster. At this stage it is recommended to configure your required Kubernetes `ClusterRole` and `ClusterRoleBinding`s, permitting other users to access the cluster.


## Cleanup

To safely delete a cluster, you must unregister it from GKE Hub before destroying the GCP infrastructure, otherwise you will leave orphaned fleet resources in your project.

```bash
# Gracefully reset and unregister the cluster
cd ${REPO_BASE}/ansible
ansible-playbook cleanup.yaml -e "cluster_name=${CLUSTER_NAME}"

# Destroy the cluster VM infrastructure
cd ${REPO_BASE}/terraform/cluster

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=clusters/${CLUSTER_NAME}/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform destroy -var="cluster_name=${CLUSTER_NAME}"
```


## End-to-end Cluster Validation and Conformance

This project leverages [Kyverno Chainsaw](https://kyverno.github.io/chainsaw/0.2.3/) to validate that the GEM cluster enforces the complex constraints and behaviors of a real GDC Connected environment.

### Running Tests
To run the full test suite against your active cluster:
```bash
cd ${REPO_BASE}/tests/e2e

chainsaw test --config chainsaw-configuration.yaml
```

## Design Goals

The GEM project was created with the following core objectives:

*   **High Fidelity Emulation**: Accurately replicate the specific behaviors, constraints, and networking topologies of a physical GDC Connected Servers environment entirely within virtualized GCP infrastructure.
*   **GDC Configuration Parity**: Apply existing GDC configurations and workload manifests to a GEM environment without any changes, and expect identical workload behavior.
*   **Accelerated Development & Prototyping**: Provide a low-friction environment for developers and operators to test GDC workloads, validate designs, and perform end-to-end validation without needing access to physical hardware.
*   **Isolation and Multi-Tenancy**: Support the deployment of multiple, fully isolated GEM clusters within a single GCP project, enabling parallel development and testing.

## v1.0 Roadmap

### Gateway API on Secondary Networks

Standard Kubernetes Services and native Gateway APIs are traditionally limited to the primary interface (`eth0`) and are unaware of secondary interfaces attached via Multus. In GDC connected environment, this is addressed using proprietary CRDs such as `GKEGatewayCIDR`, `GKEL4Route`, and `GKEEndpointSelector`.

To emulate this behavior in GEM, future work will implement:

* **Support for Secondary L2 networks**: Add support for up to 10 secondary L2 networks within a single GEM cluster.

* **Strict API Emulation**: Develop a custom translation controller to watch for proprietary GDC multi-network resources and translate them into functional open-source proxy configurations.


### Better Ingress for Networking within the Cluster

As GEM is running within GCP, the methods used to access the Services running within a GDC cluster will differ from GDC.

As an example, becuase GDC is integrated within an on-prem network and has a MetalLB VIP pool with network-routeable
IP addresses (e.g. 192.168.200.0/28) a GDC end-user will simply connect to http://192.168.200.x to access their application
running on GDC.

This exact access pattern will not be possible with GEM, but a easy to use and reliable remote access tunnel will be developed, enabling seamless end-user connectivity to all services running in the cluster. This will include at a minimum:
* HTTP(S)
* RDP
* VNC
* SSH

### Automated GEM cluster provisioning and management
To manage the GEM cluster lifecycle for many hundreds of clusters, GEM will leverage the
existing [Edge Parameter Store](https://github.com/GDC-ConsumerEdge/parameter-store) and
[Automated Cluster Provisioner](https://github.com/GDC-ConsumerEdge/automated-cluster-provisioner)
projects. Integration with these two projects will enable self-service cluster management
and fully-automated cluster creation and deletion.
