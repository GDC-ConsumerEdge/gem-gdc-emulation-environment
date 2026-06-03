
# Manually Configuring your GCP Project for GEM

This document provides explicit, step-by-step instructions to manually configure a Google Cloud Platform (GCP) project to run the GEM Emulation Environment, serving as a manual fallback to the automated [`project-setup.sh`](../project-setup.sh) script. The steps within this document assume that you're are configuring a new GCP project exclusively for GEM.


## Enabling Required GCP Service APIs

Before provisioning any cloud resource, GCP requires its corresponding Service API to be activated within your project. Enabling these APIs establishes the API endpoints and binds the default resource quotas.

### Required APIs
*   `cloudresourcemanager.googleapis.com`: Required to query, validate, and update GCP Project IAM policy bindings dynamically.
*   `serviceusage.googleapis.com`: Allows Terraform and gcloud to query active service endpoints and resolve project quotas.
*   `iamcredentials.googleapis.com`: Crucial for generating short-lived credentials and enabling Service Account token impersonation.
*   `compute.googleapis.com`: Enables Compute Engine resources, including VPCs, subnets, firewalls, and cluster nodes.
*   `gkeconnect.googleapis.com` & `gkehub.googleapis.com`: Essential for registering cluster fleet memberships and managing hybrid environments.
*   `connectgateway.googleapis.com`: Dynamically routes external `kubectl` commands to your isolated private clusters over secure Google-managed gateway endpoints.

### Manual Execution
Run this command from your local terminal to activate all required APIs:

```bash
gcloud services enable \
  cloudresourcemanager.googleapis.com \
  serviceusage.googleapis.com \
  iamcredentials.googleapis.com \
  compute.googleapis.com \
  anthos.googleapis.com \
  anthosaudit.googleapis.com \
  anthosconfigmanagement.googleapis.com \
  anthosgke.googleapis.com \
  connectgateway.googleapis.com \
  container.googleapis.com \
  gkeconnect.googleapis.com \
  gkehub.googleapis.com \
  gkeonprem.googleapis.com \
  iam.googleapis.com \
  iap.googleapis.com \
  kubernetesmetadata.googleapis.com \
  logging.googleapis.com \
  monitoring.googleapis.com \
  networkmanagement.googleapis.com \
  opsconfigmonitoring.googleapis.com \
  stackdriver.googleapis.com \
  --project="${PROJECT_ID}"
```

### Verification
*   **GCP Console**: Go to **APIs & Services $\rightarrow$ Enabled APIs & services**.
*   Verify that all listed APIs appear with a green checkmark indicating `Enabled`.


## Creating a Terraform Remote State Storage Bucket

Terraform uses a state file (`.tfstate`) to map real-world GCP resources to your configuration. Storing this state file in a Google Cloud Storage (GCS) bucket ensures team consistency and prevents resource drift.

Managing state inside a GCS bucket with Object Versioning enabled guarantees:
*  Consistent State: State is centralized and locked during runs.
*  Point-in-time recovery: Every state update creates an incremental historical version, allowing you to recover state in the event of corruption.

### Manual Execution
Create the GCS bucket and activate versioning:

```bash
# Create the storage bucket in us-central1
gcloud storage buckets create "gs://${TF_STATE_BUCKET}" \
  --project="${PROJECT_ID}" \
  --location="us-central1"

# Enable Object Versioning on the bucket
gcloud storage buckets update "gs://${TF_STATE_BUCKET}" \
  --versioning
```

### Verification
*   **GCP Console**: Go to **Cloud Storage $\rightarrow$ Buckets**.
*   Verify `gs://${TF_STATE_BUCKET}` is created, resides in `us-central1`, and shows `Object Versioning: Enabled`.


## Establishing the Provisioner Service Account

Provisioning GCE compute nodes and managing VPC networks requires privileged access. Rather than executing these builds directly under your personal account, we create a dedicated least-privilege provisioning Service Account (`tf-provisioner`).

### Required Roles
*   `roles/editor`: Grants standard creation permissions across Compute, Storage, and Network resources.
*   `roles/iam.serviceAccountAdmin`: Allows Terraform to create, manage, and assign roles to the cluster-level service accounts (`baremetal-gcr`).
*   `roles/compute.admin`: Required to manage disks, firewalls, VPC networks, and boot GCE VMs with nested virtualization.
*   `roles/resourcemanager.projectIamAdmin`: Allows the provisioner to bind IAM roles to service accounts.
*   `roles/serviceusage.serviceUsageAdmin`: Allows Terraform to audit and toggle project-level Service APIs.

### Secure Token Impersonation vs. JSON Keys
As designed, GEM restricts the generation and export of unmanaged, long-lived JSON Service Account Keys. JSON keys are a massive security risk; if leaked to a public repository they grant attackers full access to your project.

Instead, we use Service Account Impersonation via Token Creator bindings. By granting your GCP user account the `roles/iam.serviceAccountTokenCreator` role on `tf-provisioner`, GCP can generate short-lived (1-hour), auto-rotating OAuth2 tokens for the provisioning SA.

The Token Creator binding only *permits* impersonation; it does not enable it. Terraform's `google` provider impersonates `tf-provisioner` only when the `GOOGLE_IMPERSONATE_SERVICE_ACCOUNT` environment variable is set. The `impersonate_service_account` value passed to `terraform init -backend-config` applies solely to remote state access in GCS, not to the resource API calls the provider makes. Export the variable before running any `terraform` command:
```bash
export GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="${PROVISIONING_SA_EMAIL}"
```
Without this env variable set, Terraform falls back to your user credentials, and resource
operations fail with `403` permission errors even though `tf-provisioner` holds the
required roles.

### Manual Execution
1.  Create the Service Account:
    ```bash
    gcloud iam service-accounts create "tf-provisioner" \
      --display-name="Terraform Provisioning SA for GEM" \
      --project="${PROJECT_ID}"
    ```
2.  Bind Project-Level Permissions:
    ```bash
    PROVISIONING_SA_EMAIL="tf-provisioner@${PROJECT_ID}.iam.gserviceaccount.com"
    ROLES=(
      "roles/editor"
      "roles/iam.serviceAccountAdmin"
      "roles/compute.admin"
      "roles/resourcemanager.projectIamAdmin"
      "roles/serviceusage.serviceUsageAdmin"
    )

    for role in "${ROLES[@]}"; do
      gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
        --member="serviceAccount:${PROVISIONING_SA_EMAIL}" \
        --role="${role}" \
        --condition=None
    done
    ```
3.  Grant Impersonation Binding to Your GCP User Account:
    ```bash
    USER_EMAIL=$(gcloud config get-value account)
    gcloud iam service-accounts add-iam-policy-binding "${PROVISIONING_SA_EMAIL}" \
      --member="user:${USER_EMAIL}" \
      --role="roles/iam.serviceAccountTokenCreator" \
      --project="${PROJECT_ID}"
    ```

### Verification
*   **GCP Console**: Go to **IAM & Admin $\rightarrow$ Service Accounts**.
*   Verify `tf-provisioner` exists and shows your user account authorized under its *Permissions* tab as a Token Creator.


## Configuring the Core Foundation VPC Network

GEM emulates GDC Connected by establishing a fully isolated Virtual Private Cloud (VPC) network (`gem-clusters-vpc`).

```
                    GEM FOUNDATION NETWORKING ARCHITECTURE

         [ GCP VPC: gem-clusters-vpc | Subnet: gem-clusters-subnet (10.10.0.0/24) ]

                     +------------------+      +------------------+
                     |   Admin WS VM    |      |  Cluster Node 1  |
                     |  (10.10.0.2/24)  |      | (10.10.0.213/24) |
                     +--------+---------+      +--------+---------+
                              |                         |
  ============================+=========================+============================
                                           |
                             +-------------v-------------+
                             |   Cloud NAT Egress Gate   |
                             |    (Internet Egress Only) |
                             +-------------+-------------+
                                           |
                                    [ PUBLIC INTERNET ]
```

### Required Network Resources

#### VPC Network (`gem-clusters-vpc`)
A custom VPC network with automatic subnetwork creation **disabled** (`auto-create-subnetworks=false`). This guarantees strict subnet boundary isolation.

#### Subnetwork (`gem-clusters-subnet`)
A dedicated private subnet residing in `us-central1` using the CIDR IP block `10.10.0.0/24`. This subnet houses the admin workstation and cluster worker nodes.

#### Cloud NAT & Cloud Router
Cluster worker nodes and the admin workstation reside in a private network with no public IP addresses assigned to prevent internet-facing attacks. However, these machines must be able to reach the internet to:
*  Fetch operating system security updates (`apt`).
*  Download versioned Anthos Bare Metal binaries (`bmctl`).
*  Download the software required for the GEM platform.

We provision a Cloud Router (`gem-clusters-vpc-router`) and map a Cloud NAT gateway (`gem-clusters-vpc-nat`) to it. Cloud NAT dynamically translates private IP addresses to public Google egress IPs for outbound traffic, allowing nodes internet egress without ever exposing them to inbound internet access.

#### Firewall: Allow Internal VPC Traffic (`gem-clusters-allow-internal`)
Anthos Bare Metal nodes communicate heavily over internal control planes (etcd, Kubelet API, VXLAN overlay routing, MetalLB GARP flooding). We create a firewall rule allowing all TCP, UDP, and ICMP traffic internally within the `10.10.0.0/24` block for instances matching the target tags `["http-server", "https-server"]`.

#### Firewall: Allow Secure GCP IAP SSH Tunneling (`gem-clusters-allow-iap-ssh`)
Because instances do not have public IP addresses, you cannot SSH into them directly over the internet.

Instead, you use Google Cloud's Identity-Aware Proxy (IAP). IAP acts as an encrypted SSH proxy. We create a firewall rule allowing TCP port 22 ingress **strictly** from the official Google IAP Proxy IP range **`35.235.240.0/20`** matching target tags `["http-server", "https-server"]`. Any inbound packet originating from outside this range is instantly dropped by GCP.

### Manual Execution
1.  **Create the VPC Network**:
    ```bash
    gcloud compute networks create "gem-clusters-vpc" \
      --project="${PROJECT_ID}" \
      --subnet-mode=custom
    ```
2.  **Create the Subnetwork**:
    ```bash
    gcloud compute networks subnets create "gem-clusters-subnet" \
      --project="${PROJECT_ID}" \
      --network="gem-clusters-vpc" \
      --region="us-central1" \
      --range="10.10.0.0/24"
    ```
3.  **Create the Internal Firewall Rule**:
    ```bash
    gcloud compute firewall-rules create "gem-clusters-allow-internal" \
      --project="${PROJECT_ID}" \
      --network="gem-clusters-vpc" \
      --allow=tcp,udp,icmp \
      --source-ranges="10.10.0.0/24" \
      --target-tags="http-server","https-server"
    ```
4.  **Create the IAP SSH Firewall Rule**:
    ```bash
    gcloud compute firewall-rules create "gem-clusters-allow-iap-ssh" \
      --project="${PROJECT_ID}" \
      --network="gem-clusters-vpc" \
      --allow=tcp:22 \
      --source-ranges="35.235.240.0/20" \
      --target-tags="http-server","https-server"
    ```
5.  **Create the Cloud Router**:
    ```bash
    gcloud compute routers create "gem-clusters-vpc-router" \
      --project="${PROJECT_ID}" \
      --network="gem-clusters-vpc" \
      --region="us-central1"
    ```
6.  **Create and Bind the Cloud NAT Gateway**:
    ```bash
    gcloud compute routers nats create "gem-clusters-vpc-nat" \
      --project="${PROJECT_ID}" \
      --router="gem-clusters-vpc-router" \
      --region="us-central1" \
      --auto-allocate-nat-external-ips \
      --nat-all-subnet-ip-ranges
    ```

### Verification
*   **GCP Console**: Go to **VPC network $\rightarrow$ VPC networks**.
*   Verify `gem-clusters-vpc` exists, contains subnet `gem-clusters-subnet` with IP range `10.10.0.0/24`, shows the two firewall rules active, and has `gem-clusters-vpc-nat` listed as active under **Network services $\rightarrow$ Cloud NAT**.


## Spawning the Fleet Registry & GCR IAM Roles

Running GDC Connected emulation requires two additional dedicated service accounts to manage image registries and register control plane gateway endpoints.

### Required SAs

#### 1. Anthos Bare Metal Pull/Push SA (`baremetal-gcr`)
*   This SA runs inside the cluster nodes. It is responsible for pulling official Google Anthos images from the GCR container registry, exporting cluster telemetry, and establishing fleet membership connection gates.
*   Required Roles:
    *   `roles/gkehub.connect`: Allows GKE Connect agents running inside the cluster nodes to register fleet membership.
    *   `roles/gkehub.admin`: Grants admin permissions to establish memberships in the fleet registry.
    *   `roles/logging.logWriter` & `roles/monitoring.metricWriter`: Enables log/metric streaming.
    *   `roles/compute.viewer`: Allows node agents to inspect VM resources.

#### 2. Cluster Administrator SA (`gem-cluster-admin`)
*   This service account is used to administer the active GEM cluster remotely via GKE
Connect Gateway. It is bound to the Kubernetes `cluster-admin` ClusterRole, granting
unrestricted administrative control over cluster resources. Routing remote connections
through this service account ensures that any developer authorized to impersonate this
service account can securely connect to and manage the cluster directly from their local
workstation.

*   Required Roles:
    *   `roles/gkehub.gatewayAdmin`: Allows the service account to dynamically authenticate through GKE Connect Gateway.
    *   `roles/gkehub.admin`: Required to manage GKE Hub memberships.

### Manual Execution
1.  Create the `baremetal-gcr` Service Account:
    ```bash
    gcloud iam service-accounts create "baremetal-gcr" \
      --display-name="Service Account for Anthos Bare Metal" \
      --project="${PROJECT_ID}"
    ```
2.  Bind `baremetal-gcr` Project Permissions:
    ```bash
    BAREMETAL_SA_EMAIL="baremetal-gcr@${PROJECT_ID}.iam.gserviceaccount.com"
    BAREMETAL_ROLES=(
      "roles/gkehub.connect"
      "roles/gkehub.admin"
      "roles/logging.logWriter"
      "roles/monitoring.metricWriter"
      "roles/monitoring.dashboardEditor"
      "roles/stackdriver.resourceMetadata.writer"
      "roles/opsconfigmonitoring.resourceMetadata.writer"
      "roles/kubernetesmetadata.publisher"
      "roles/compute.viewer"
      "roles/serviceusage.serviceUsageViewer"
    )

    for role in "${BAREMETAL_ROLES[@]}"; do
      gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
        --member="serviceAccount:${BAREMETAL_SA_EMAIL}" \
        --role="${role}" \
        --condition=None
    done
    ```
3.  Create the `gem-cluster-admin` Service Account:
    ```bash
    gcloud iam service-accounts create "gem-cluster-admin" \
      --display-name="GEM Cluster Admin" \
      --project="${PROJECT_ID}"
    ```
4.  Bind `gem-cluster-admin` Project Permissions:
    ```bash
    ADMIN_SA_EMAIL="gem-cluster-admin@${PROJECT_ID}.iam.gserviceaccount.com"
    ADMIN_ROLES=(
      "roles/gkehub.gatewayAdmin"
      "roles/gkehub.admin"
    )

    for role in "${ADMIN_ROLES[@]}"; do
      gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
        --member="serviceAccount:${ADMIN_SA_EMAIL}" \
        --role="${role}" \
        --condition=None
    done
    ```

### Verification
*   GCP Console: Go to **IAM & Admin $\rightarrow$ Service Accounts**.
*   Verify both `baremetal-gcr` and `gem-cluster-admin` exist and are assigned their respective project IAM role bindings under **IAM & Admin $\rightarrow$ IAM**.


## Local State Configuration (`backend.tf` and `terraform.tfvars`)

Now that the GCP project APIs, storage buckets, service accounts, and VPC networks have been created, you must create the local variables and backend mapping files inside your cloned repository so Terraform can synchronize remote state correctly.

### 1. Generate Local variable Files (`terraform.tfvars`)
Navigate to each Terraform directory and create the required `terraform.tfvars` file:

```bash
# Foundation Var File
cat <<EOF > "${REPO_ROOT}/terraform/foundation/terraform.tfvars"
project_id            = "${PROJECT_ID}"
EOF

# Workstation Var File
cat <<EOF > "${REPO_ROOT}/terraform/admin-workstation/terraform.tfvars"
project_id            = "${PROJECT_ID}"
EOF

# Edge Router Var File
cat <<EOF > "${REPO_ROOT}/terraform/edge-router/terraform.tfvars"
project_id            = "${PROJECT_ID}"
EOF

# Cluster Var File
cat <<EOF > "${REPO_ROOT}/terraform/cluster/terraform.tfvars"
project_id            = "${PROJECT_ID}"
provisioning_sa_email = "${PROVISIONING_SA_EMAIL}"
cluster_name          = "${CLUSTER_NAME}"
EOF
```

### 2. Generate Remote Backend Files (`backend.tf`)
Configure Terraform to use your newly created GCS bucket for remote state management instead of local disk:

```bash
# Foundation Backend
cat <<EOF > "${REPO_ROOT}/terraform/foundation/backend.tf"
terraform {
  backend "gcs" {}
}
EOF

# Workstation Backend
cat <<EOF > "${REPO_ROOT}/terraform/admin-workstation/backend.tf"
terraform {
  backend "gcs" {}
}
EOF

# Edge Router Backend
cat <<EOF > "${REPO_ROOT}/terraform/edge-router/backend.tf"
terraform {
  backend "gcs" {}
}
EOF

# Cluster Backend
cat <<EOF > "${REPO_ROOT}/terraform/cluster/backend.tf"
terraform {
  backend "gcs" {}
}
EOF
```


Your GCP project is now set up and ready to deploy the foundation and admin workstation as detailed in the [GEM README](../README.md#deploy-foundation-and-admin-workstation).
