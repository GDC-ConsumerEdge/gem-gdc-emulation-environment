# Agent Guide: GEM (GDC EMulation Environment)

This file provides essential context, architectural details, and recent updates for AI agents working on the GEM project. Read this file to quickly and accurately understand the project state and constraints.

## 🎯 Project Goal
GEM emulates a physical Google Distributed Cloud (GDC) Connected bare-metal environment entirely on Google Compute Engine (GCE) instances. It is used for rapid prototyping, testing, and validation of GDC workloads without requiring physical hardware.

## 📚 Documentation Links
If you are unsure about how a specific technology works or have questions about its usage in this project, refer to the official documentation links below:

*   **Google Distributed Cloud Connected**: [GDC Connected Docs](https://docs.cloud.google.com/distributed-cloud/docs)
*   **Google Distributed Cloud (Software-only) for bare metal**: [GDC Bare Metal Docs](https://docs.cloud.google.com/kubernetes-engine/distributed-cloud/bare-metal/docs)
*   **Google Compute Engine (GCE)**: [GCE Docs](https://cloud.google.com/compute/docs)
*   **Terraform**: [Terraform Docs](https://developer.hashicorp.com/terraform/docs)
*   **Ansible**: [Ansible Docs](https://docs.ansible.com/)
*   **Kyverno Chainsaw** (Testing): [Chainsaw Docs](https://kyverno.github.io/chainsaw/)
*   **OPA Gatekeeper** (Policy Enforcement): [Gatekeeper Docs](https://open-policy-agent.github.io/gatekeeper/website/docs/)
*   **TopoLVM** (Storage): [TopoLVM Docs](https://github.com/topolvm/topolvm)
*   **Traefik** (Edge Router): [Traefik Docs](https://doc.traefik.io/traefik/)

## 🏗️ Architecture & Topology
*   **Foundation (`terraform/foundation`)**: Base VPC (`gem-clusters-vpc`), subnets, Cloud NAT, and APIs.
*   **Admin Workstation (`terraform/admin-workstation`)**: A dedicated VM (`gem-admin-ws`) used to run `bmctl` and manage clusters.
*   **GEM Clusters (`terraform/cluster`)**: 3-node GDC-like environments running on GCE VMs with nested virtualization.
*   **Edge Router (`terraform/edge-router`)**: An optional VM running Traefik to proxy traffic to MetalLB VIPs on secondary networks.

## 🔌 Networking & Emulation
*   **VXLAN Overlay**: L2 secondary networks are emulated using VXLAN on top of the GCP VPC.
*   **Naming Convention**:
    *   Secondary interfaces are named `gdcenet0.<vlan_id>` on cluster nodes to allow native GDC `Network` CRs to discover them without modification.
    *   Primary VXLAN interfaces are named `vx-<truncated_cluster_name>` (e.g., `vx-gemcluster2`).
    *   Secondary interfaces on the workstation are named `sec-<truncated_cluster_name>-<vlan_id>` (e.g., `sec-gem111-123`).
*   **MTU Constraints**: GCP VPC has a 1460 MTU limit. VXLAN adds 50 bytes overhead. All overlay interfaces **must** use an MTU of **1410**.
*   **TCP MSS Clamping**: Strictly required on the primary VXLAN interface to prevent TLS EOF errors on large payloads.

## 💾 Storage & Policies
*   **TopoLVM**: Used as the storage provider on the nodes.
*   **Gatekeeper Mutations**: Since usage of unmodified GDC workload configurations is a requirement, and Symcloud Storage (Robin) is the default storage provider in GDC, OPA Gatekeeper mutations are used to:
    *   Translate `StorageClass` requests for `robin` to `topolvm.io`.
    *   Downgrade PVC requests from `ReadWriteMany` to `ReadWriteOnce` as TopoLVM only support RWO storage.

## 🚀 Key Workflows & Recent Changes
*   **Dynamic Inventory**: `ansible/inventory.sh` reads state directly from **GCS** (`gs://gem-${PROJECT_ID}-tfstate/...`). It does **not** rely on local Terraform state files. Ensure `PROJECT_ID` is set in your environment or available via `gcloud`.
*   **Automated SSH Key Gen**: `ansible/create-cluster.yaml` includes a local play at the top to generate `~/.ssh/google_compute_engine` via `gcloud compute config-ssh --quiet` if it is missing.
*   **Robust Partitioning**: `sleep` commands for waiting for device files in `/dev` have been replaced with `udevadm settle` in both cloud-init and Ansible tasks.
*   **Dynamic Version Mapping**: Added `emulate_gdc_version` in Ansible to map GDC versions to specific Anthos Bare Metal versions, allowing multi-version testing.
*   **Multi-Version `bmctl`**: The workstation now manages multiple `bmctl` binaries in `/usr/local/sbin/` and symlinks the active one dynamically based on the requested version.
*   **Idempotent VXLAN**: The VXLAN service task was updated to use `state: started` to prevent interface flapping on repeated playbook runs.

## ⚠️ Known Gotchas
*   **QEMU Fallback**: If the GCP project enforces Shielded VMs (Secure Boot), nested virtualization (KVM) fails. The cluster falls back to QEMU software emulation, which breaks Windows VM scheduling due to lack of Hyper-V features. Workaround: Set `osType: Linux` for all VMs if using QEMU.
*   **Fragile Coupling**: Partition sizes are hardcoded in both Terraform (`compute.tf` creates 100GB partition) and Ansible (`cluster_nodes` creates TopoLVM partition starting at 100GB). If you change one, you must change the other.
*   **Hostname Assumption**: The VXLAN script assumes hostnames end in numbers (e.g., `node1`) to calculate IP octets. Do not change naming without updating the script.
*   **Storage Capacity vs. Usable Emulation**: The cluster provides ~3.9 TB of aggregate raw storage (1.3 TB per node via TopoLVM). Real GDC clusters with Robin SDS typically yield only ~1.3 TB of usable space due to 3-way replication. This limit is not currently enforced in the emulation to allow testing larger single volumes up to the node limit.

## 🗺️ Future Work
*   **Gateway API on Secondary Networks**: Native Gateway APIs don't see Multus secondary interfaces. Future work involves either strict API emulation of proprietary GDC CRDs or functional emulation using OSS Gateway API with an EndpointSlice mutator.
*   **CI/CD Pipeline**: Set up a GitHub Action to run `pre-commit` on every pull request to enforce linting, formatting, and license validation (configuration is already complete in `.pre-commit-config.yaml`).
