# GEM Admin Workstation

The GEM Admin Workstation (`gem-admin-ws`) is a dedicated Google Compute Engine (GCE) virtual machine that serves as the centralized control point and management node for the GDC emulation environment (GEM). It is responsible for orchestrating the lifecycle of GEM workload clusters, running the `bmctl` deployment engine, managing credentials and fleet registrations, and serving as a bootstrap node. The GEM Admin Workstation isolates cluster installation tasks and orchestration software from both the local developer machine and the cluster nodes themselves.


## Design Decisions

### Isolation of the GDC (software only) Installer
Running the Anthos Bare Metal bootstrap cluster (`bmctl`) requires spinning up a local KinD (Kubernetes-in-Docker) cluster on the installer machine. The Admin Workstation is built as a standalone, isolated GCE VM to ensure cluster bootstrap operations never interfere with installer execution.

### Dynamic Ansible Orchestration
The workstation relies on a dynamic inventory script (`ansible/inventory.sh`) that compiles live infrastructure state directly from **Google Cloud Storage (GCS)** Terraform state files. This prevents hardcoding of node IPs and allows the workstation to interact seamlessly with multiple, ephemeral GDC emulation clusters without manual configuration updates.

### Host-Specific VXLAN Persistence
The Admin Workstation must participate in the VXLAN overlay network (`vx-*`, `sec-*`) to directly communicate with the emulation nodes. To allow the workstation to be completely ephemeral, all generated `.netdev` and `.network` interface configurations are synchronized in real-time to a GCS bucket (`gs://gem-${PROJECT_ID}-overlay-sync/gem_admin_ws/`). A background cron job runs every minute on the workstation to pull down configurations, ensuring that if the workstation is rebuilt, it immediately recovers network connectivity to all running GEM clusters.

## Installation and Configuration

The bootstrap of the Admin Workstation is split into a two-tier process: provisioning via Terraform and OS-level configuration via Ansible.

### 1. Provisioning (Terraform)
Run from the root repository directory on your local machine to provision the GCE instance:
```bash
cd terraform/admin-workstation
terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=admin-workstation/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply
```

### 2. Configuration (Ansible)
Run the playbook to bootstrap the OS, install dependencies, and configure services:
```bash
cd ansible
CLUSTER_NAME=none ansible-playbook -i inventory.sh admin-workstation.yaml
```
*Note: Setting `CLUSTER_NAME=none` ensures the playbook only provisions workstation-specific dependencies and does not attempt to connect to a non-existent cluster.*

During configuration, Ansible will automatically:
*   Perform an OS `apt update` and `dist-upgrade` (triggering a synchronous system reboot if a new Linux kernel is installed).
*   Install `kubectl`, `docker`, and the `bmctl` binaries.
*   Generate an SSH key pair and authorize it for inbound SSH cluster communications.
*   Configure the dynamic VXLAN overlay synchronizer Cron job (executing the sync script every minute).



## Troubleshooting

### VXLAN Interface Sync Failures
If the workstation loses connection to the cluster overlay network (`10.200.X.X` IPs):
*   **Symptom**: Pinging cluster nodes via their overlay IPs (`10.200.X.2-4`) fails with `Destination Host Unreachable` or timeouts.
*   **Diagnostics**:
    1.  Check if the synchronizer cron job is scheduled:
        ```bash
        sudo crontab -l
        ```
    2.  Manually trigger the sync script to check for errors:
        ```bash
        sudo PROJECT_ID=your-project-id HOST_DIR=gem_admin_ws /usr/local/sbin/gem-cron-overlay-sync.sh
        ```
    3.  Check the GCS bucket to make sure files are present:
        ```bash
        gcloud storage ls gs://gem-your-project-id-overlay-sync/gem_admin_ws/
        ```

### Tunnel Stale Cache (Destination Host Unreachable)
If you deleted and rebuilt the Admin Workstation VM and it received a new dynamic underlay IP from GCP, the VXLAN mesh FDB tables will be broken.
*   **Symptom**: The workstation can reach underlay IPs but overlay communication is completely broken.
*   **Resolution**: Re-run the `restore-vxlan.yaml` playbook to update the FDB peer mapping across the entire mesh:
    ```bash
    CLUSTER_NAME=your-cluster-name ansible-playbook -i inventory.sh restore-vxlan.yaml
    ```
