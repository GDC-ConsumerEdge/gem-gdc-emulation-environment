<h1 align=center>GEM - GDC EMulation Environment</h1>
</p>
<p align=center>
<img src="docs/img/gem-logo.png" height="250"/>
</p>

GEM, the GDC EMulation Environment is a Google Distributed Cloud Connected (GDC)
like platform running entirely on Google Compute Engine. It accurately mimics a
physical GDC Connected environment using isolated virtual resources in GCP,
allowing for rapid prototyping, architecture and development testing, and robust
end-to-end validation of GDC workloads. GEM is **not** a
[Google Distributed Cloud Connected](https://cloud.google.com/distributed-cloud-connected)
variant, rather it uses a opinionated build of
[Google Distributed Cloud (software only) for bare metal](https://docs.cloud.google.com/kubernetes-engine/distributed-cloud/bare-metal/docs/concepts/about-bare-metal)
to accurately emulate GDC Connected Servers.

Existing GDC configurations and workload manifests apply to a GEM environment
unchanged, and behave the same way they do on physical hardware. This
intentional parity lets you validate designs and test workloads without access
to a real GDC Connected deployment.

## GEM Design

The GEM platform takes a modular design approach to isolate stable, foundational
infrastructure from ephemeral GEM workload clusters:

- **Foundation (`terraform/foundation`)**: Provisions the core VPC networks
  (`gem-clusters-vpc`), subnets, Cloud NAT, and foundational Service Accounts
  required to run the environment.
- **Admin Workstation (`terraform/admin-workstation`)**: A dedicated, GCE
  instance (`gem-admin-ws`) used to build and manage the GEM workload clusters.
- **Edge Router (`terraform/edge-router`)**: An optional VM attached to both the
  VPC and every cluster's overlay network, which developers tunnel through to
  reach Kubernetes Services running within the GEM cluster.
- **GEM Clusters (`terraform/cluster`)**: A dedicated, 3-node GDC-like
  environment, used to run Kubernetes workloads, including virtual machines with
  VMRuntime. Multiple isolated GEM clusters can be deployed in the same GCP
  project.

## Getting Started

### Prerequisites

- [Google Cloud SDK](https://cloud.google.com/sdk) (`gcloud`) installed,
  authenticated and configured.
  - Ensure your Google Cloud SDK
    [Application Default Credentials](https://docs.cloud.google.com/docs/authentication/application-default-credentials)
    are also configured
- [HashiCorp Terraform CLI](https://developer.hashicorp.com/terraform/install)
  (`terraform`) installed.
- [Ansible](https://docs.ansible.com/projects/ansible/latest/installation_guide/intro_installation.html)
  (`ansible-playbook`) installed.
- [jq](https://jqlang.org/download/) installed.
- This
  [GEM repo](https://github.com/GDC-ConsumerEdge/gem-gdc-emulation-environment)
  cloned to a machine used to provision a GEM environment. To get started, this
  can be your local workstation.

### Environment Setup

Some initial setup needs to be completed before you can begin to provision GEM
clusters. These environment variables will be used throughout the GEM creation
process:

```bash
export CLUSTER_NAME=gem-cluster-1
export PROJECT_ID=your-gcp-project-id
export TF_STATE_BUCKET=gem-${PROJECT_ID}-tfstate

# The GCP zone into which GEM instances will be provisioned.
export GEM_GCP_ZONE=us-east4-b

# The local directory of this repo
export REPO_ROOT=~/src/gem-gdc-emulation-environment

# A GCP Service Account used by Terraform to provision the GEM infrastructure
export PROVISIONING_SA_EMAIL="tf-provisioner@${PROJECT_ID}.iam.gserviceaccount.com"

# Make the Terraform google provider impersonate the provisioning SA for all
# resource operations. The -backend-config impersonation flag used below only
# covers remote state access in GCS, not the API calls that create resources.
export GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="${PROVISIONING_SA_EMAIL}"

# A GCP Service Account used by Ansible to build a GEM cluster
export IMPERSONATE_SA_EMAIL="gem-cluster-admin@${PROJECT_ID}.iam.gserviceaccount.com"
```

### Configure your GCP Project

Initial setup of a GCP project needs to be completed before you can provision
GEM resources. This includes:

- Enable initial required GCP APIs
- Creation of a dedicated Google Cloud Storage bucket to manage Terraform state
- Creation of a provisioner GCP Service account, which is used to create the
  various compute resources throughout the project.

A helper script is provided to automate this work:

```bash
# Creates the SA, grants permissions, generates backend.tf and tfvars files
cd ${REPO_ROOT}
./project-setup.sh
```

If you wish to configure your GCP project manually, or to better understand what
the `project-setup.sh` script is doing, refer to the
[Project Setup](docs/project-setup.md) documentation.

### Deploy Foundation and Admin Workstation

*This should only be required once per GCP project.*

```bash

# Build and deploy the GEM foundation
cd ${REPO_ROOT}/terraform/foundation

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=foundation/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply

# Deploy the Admin Workstation
cd ${REPO_ROOT}/terraform/admin-workstation

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=admin-workstation/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply

# Configure the Admin Workstation
cd ${REPO_ROOT}/ansible

ansible-playbook admin-workstation.yaml
```

### Deploy the GEM Edge Router

To access services running inside your GEM cluster including HTTP, RDP, VNC, or
other TCP-based protocols, the GEM Edge Router is the host through which this
traffic will pass.

The Edge Router has network connectivity to each GEM cluster in your
environment, including to all VXLAN secondary networks. Nothing is exposed
directly: you reach those addresses by opening SSH port forwards through the
Edge Router with [`scripts/gem-tunnel.sh`](scripts/gem-tunnel.sh), which turns
its connectivity into reachability from your local workstation.

```bash
cd ${REPO_ROOT}/terraform/edge-router

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=edge-router/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply

cd ${REPO_ROOT}/ansible
ansible-playbook edge-router.yaml
```

### Provision and Deploy a GEM Cluster

You can deploy as many isolated GEM clusters as your GCP quota allows by
changing the `CLUSTER_NAME`.

GEM provides emulation for the two most recent major versions of GDC. This can
be specified at build time through the `emulate_gdc_version` variable. If the
`emulate_gdc_version` is not specified, the most recent GDC version will be
emulated. Available options can be found in
[ansible/group_vars/all.yaml](ansible/group_vars/all.yaml)

The cluster build process takes approximately 30 minutes to complete.

```bash
# Provision the 3 Compute Engine nodes
cd ${REPO_ROOT}/terraform/cluster

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=clusters/${CLUSTER_NAME}/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

# Provision the GEM cluster nodes using the g2-small-64gb hardware variant (the default)
terraform apply -var="cluster_name=${CLUSTER_NAME}"

# Or, provision the GEM cluster nodes using the g1-medium hardware variant
terraform apply -var="cluster_name=${CLUSTER_NAME}" -var="hardware_variant=g1-medium"


cd ${REPO_ROOT}/ansible

# Build a GEM cluster, emulating the latest available version
ansible-playbook create-cluster.yaml

# Or, build a GEM cluster emulating GDC version 1.12.1
ansible-playbook create-cluster.yaml --extra-vars "emulate_gdc_version=1.12.1"
```

The playbook prints progress as it runs. To watch the underlying installation in
real time, SSH into the admin workstation and tail the `bmctl` log:

```bash
# Connect to the admin workstation
# All cluster operations are run as the gem user
gcloud compute ssh gem@gem-admin-ws --tunnel-through-iap --project=${PROJECT_ID}

# Tail the build logs
tail -f ~/bmctl-workspace/${CLUSTER_NAME}/log/create-cluster-*/create-cluster.log
```

#### Automating cluster builds

The commands above are great to get started with GEM. GEM ships with both Cloud
Build pipelines and a REST API that do the same work in GCP instead. This helps
with scaled deployments, and to ensure ensure that GEM cluster builds are
repeatable and efficient.

- **Cloud Build**: on-demand build and teardown pipelines that need no local
  toolchain. See [Cloud Build](docs/cloud-build.md).
- **GEM REST API**: a FastAPI service that exposes cluster lifecycle operations
  with streaming logs and cancellation. See [GEM REST API](docs/gem-api.md).

#### GDC Hardware Configurations

GEM supports emulating official Google Distributed Cloud (GDC)
[hardware configurations](https://docs.cloud.google.com/distributed-cloud/connected/latest/docs/requirements#hardware)
directly via Terraform. Set the `hardware_variant` variable in your `cluster`
module's `terraform.tfvars` file to choose a hardware configuration, or specify
at build time with the `-var="hardware_variant="` argument.

| Hardware Variant (specifications per node) | vCPUs   | Memory | Data Disk Size |
| :----------------------------------------- | :------ | :----- | :------------- |
| `g1-medium`                                | 32 vCPU | 64 GB  | 1.6 TB SSD     |
| `g1-large`                                 | 64 vCPU | 128 GB | 3.2 TB SSD     |
| `g2-small-64gb` *(Default)*                | 32 vCPU | 64 GB  | 3.84 TB SSD    |
| `g2-small-128gb`                           | 32 vCPU | 128 GB | 3.84 TB SSD    |
| `g2-medium`                                | 48 vCPU | 128 GB | 3.84 TB SSD    |
| `g2-large`                                 | 64 vCPU | 128 GB | 3.84 TB SSD    |
| `dev-and-test`                             | 8 vCPU  | 32 GB  | 150 GB SSD     |

## Accessing Your Cluster

Once the build has finished, you can reach the cluster from the admin
workstation or from your local machine, and reach the Services running on it
through the Edge Router.

### Initial GEM Cluster Access

Once the build has finished, an admin-level kubeconfig is made available on the
Admin Workstation under
`~/bmctl-workspace/${CLUSTER_NAME}/${CLUSTER_NAME}-kubeconfig`.

The user credentials in the admin workstation kubeconfig have admin level
permissions and is not representative of the real-world permissions a typical
user will have on a GEM cluster. This is **not** the primary means of cluster
access, but should be used primarily for break-glass admin-level
troubleshooting.

```bash
kubectl get nodes --kubeconfig /home/gem/bmctl-workspace/${CLUSTER_NAME}/${CLUSTER_NAME}-kubeconfig
```

### Local Access via GKE Connect Gateway

To access the cluster remotely from your local workstation, use the GKE Connect
Gateway by impersonating the `gem-cluster-admin` service account:

```bash
export GEM_CLUSTER_ADMIN_SA_EMAIL="gem-cluster-admin@${PROJECT_ID}.iam.gserviceaccount.com"

gcloud config set auth/impersonate_service_account ${GEM_CLUSTER_ADMIN_SA_EMAIL}
gcloud container fleet memberships get-credentials ${CLUSTER_NAME}

kubectl get nodes
```

From this point forward, you have a functioning GDC-like environment, which can
be configured like any other GDC cluster. At this stage it is recommended to
configure your required Kubernetes `ClusterRole` and `ClusterRoleBinding`,
permitting other users to access the cluster.

### Access Services Running on A GEM Cluster

Access to the various Kubernetes Services running within a GEM cluster is
facilitated through the GEM Edge Router, using long-lived SSH tunnels. Your
local workstation will create an SSH tunnel using `gcloud compute ssh`, which is
a thin wrapper around ssh that takes care of authentication, translation of an
instance name into an IP address and connectivity through the GCP
[Identity-Aware Proxy](https://docs.cloud.google.com/iap/docs/concepts-overview).
All of this enables you to securely access your GEM VM instances from your local
workstation without exposing the VM instances to the internet.

Currently, only connectivity to MetalLB VIPs (Kubernetes Service of
`type: LoadBalancer`) is supported.

[`gem-tunnel.sh`](./scripts/gem-tunnel.sh) will assist in creating a secure
tunnel to the GEM Edge Router, which then forwards the traffic to MetalLB VIPs
running within a GEM cluster.

To start, identify the Service you wish to connect to:

```
k get service -n applications
NAME                         TYPE           CLUSTER-IP       EXTERNAL-IP     PORT(S)
application-webserver        LoadBalancer   10.109.51.163    10.200.145.52   80:32611/TCP
```

Once you have a Service with an External IP, you can pass that to
`gem-tunnel.sh`, either as an address or as a `namespace/service` name that
resolves with your local `kubectl`:

```
./gem-tunnel.sh --http applications/application-webserver


              \ \        💎       \ \
 ______________\ \_________________\ \_______________


 HTTP:    http://localhost:8080 → 10.200.145.52:80

 _______________  __________________  _______________
               / /                 / /
              / /                 / /


  Press Ctrl-C to disconnect
```

At this point, you are able to connect to http://localhost:8080 from your local
workstation and reach the application running in your GEM cluster:

```
curl -Is http://localhost:8080
HTTP/1.1 200 OK
Server: nginx
Content-Type: text/html; charset=utf-8
Date: Wed, 06 May 2026 19:55:46 GMT
Last-Modified: Tue, 10 Sep 2024 01:50:27 GMT
Accept-Ranges: bytes
Connection: close
Content-Length: 25416
```

GEM Tunnel has convenience flags for typical protocols like HTTP, RDP and VNC,
supports any TCP-based protocol through the `--tunnel` flag, and will open as
many destinations as you ask for over a single tunnel. See
[Reaching a service](docs/edge-router.md#reaching-a-service) for further
examples, or run `${REPO_ROOT}/scripts/gem-tunnel.sh --help`.

## Cleanup

To safely delete a cluster, you must unregister it from GKE Hub before
destroying the GCP infrastructure, otherwise you will leave orphaned fleet
resources in your project.

```bash
# Gracefully reset and unregister the cluster
cd ${REPO_ROOT}/ansible
ansible-playbook cleanup.yaml -e "cluster_name=${CLUSTER_NAME}"

# Destroy the cluster VM infrastructure
cd ${REPO_ROOT}/terraform/cluster

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=clusters/${CLUSTER_NAME}/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform destroy -var="cluster_name=${CLUSTER_NAME}"
```

## Documentation

| Document                                                                       | What it covers                                                                   |
| :----------------------------------------------------------------------------- | :------------------------------------------------------------------------------- |
| [Project Setup](docs/project-setup.md)                                         | Configuring a GCP project by hand, and what `project-setup.sh` automates         |
| [Admin Workstation](docs/admin-workstation.md)                                 | What runs on `gem-admin-ws`, how to connect, and the multi-version `bmctl` setup |
| [Edge Router](docs/edge-router.md)                                             | Reaching cluster Services, and the full `gem-tunnel.sh` reference                |
| [GEM Networking](docs/gem-networking.md)                                       | GEM networking overview, the GCP VPC layout and VXLAN overlay                    |
| [Secondary Networks](docs/secondary-networks.md)                               | Emulating GDC secondary networks and the Multi-Network Gateway API               |
| [Network Operator Implementation](docs/gem-network-operator-implementation.md) | How `gem-network-operator` reconciles those resources                            |
| [Storage](docs/storage.md)                                                     | TopoLVM, and the Gatekeeper mutations that emulate Robin SDS                     |
| [Cloud Build](docs/cloud-build.md)                                             | Building and tearing down clusters in CI                                         |
| [GEM REST API](docs/gem-api.md)                                                | The FastAPI-powered GEM REST API service                                         |
| [Code Style](docs/style.md)                                                    | Where GEM departs from each language's usual conventions                         |

To contribute to the GEM project, see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache Version 2.0

See [LICENSE](LICENSE)

## Disclaimer

This is not an official Google product.
