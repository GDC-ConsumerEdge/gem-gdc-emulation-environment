# Agent Guide: GEM (GDC EMulation Environment)

This file provides essential context, architectural details, and conventions for AI agents working on the GEM project. Read this file to quickly and accurately understand the project state and constraints.

## 🎯 Project Goal
GEM emulates a physical Google Distributed Cloud (GDC) Connected environment entirely on Google Compute Engine (GCE) instances. It is used for rapid prototyping, testing, and validation of GDC workloads without requiring physical hardware.

## 🧰 Project Type & Tooling
GEM is an infrastructure-as-code project, not an application codebase. It is built from Terraform (`terraform/`), Ansible (`ansible/`), Bash (`scripts/`, `cloudbuild/`), and Cloud Build pipelines.

Validate changes locally without provisioning real infrastructure:

*   `pre-commit run --all-files` — formatting, linting, license headers, and secret scanning.
*   `./scripts/run-unit-tests.sh` — Terraform `terraform test` plus Ansible check playbooks.

Never run `terraform apply` or live Ansible plays against real GCP resources to validate a change unless approved. Both operations create or mutate live infrastructure.

## 🛠️ Task Playbooks
Step-by-step procedures for common changes live in [skills/](skills/). Read the relevant one before making that kind of change:

*   [skills/validate.md](skills/validate.md) — validate a change locally without provisioning infrastructure.
*   [skills/add-ansible-role.md](skills/add-ansible-role.md) — scaffold and wire a new Ansible role.
*   [skills/add-terraform-module.md](skills/add-terraform-module.md) — scaffold a new Terraform module.

## 📚 Documentation Links
If you are unsure about how a specific technology works or have questions about its usage in this project, refer to the official documentation links below:

*   **Google Distributed Cloud Connected**: [GDC Connected Docs](https://docs.cloud.google.com/distributed-cloud/connected/latest/docs/overview)
*   **Google Distributed Cloud (Software-only) for bare metal**: [GDC Bare Metal Docs](https://docs.cloud.google.com/kubernetes-engine/distributed-cloud/bare-metal/docs/concepts/about-bare-metal)
*   **Google Compute Engine (GCE)**: [GCE Docs](https://docs.cloud.google.com/compute/docs/overview)
*   **Terraform**: [Terraform Docs](https://developer.hashicorp.com/terraform/docs)
*   **Ansible**: [Ansible Docs](https://docs.ansible.com/)
*   **Kyverno Chainsaw** (Testing): [Chainsaw Docs](https://kyverno.github.io/chainsaw/)
*   **OPA Gatekeeper** (Policy Enforcement): [Gatekeeper Docs](https://open-policy-agent.github.io/gatekeeper/website/docs/)
*   **TopoLVM** (Storage): [TopoLVM Docs](https://github.com/topolvm/topolvm)
*   **Traefik** (Edge Router): [Traefik Docs](https://doc.traefik.io/traefik/)

## 🏗️ Component Map
GEM is built from independent components, each provisioned by Terraform and configured by Ansible. Start here to find the right code and the deep-dive doc for the area you are changing.

| Component | Primary code | Deep-dive doc |
| :--- | :--- | :--- |
| Foundation (VPC `gem-clusters-vpc`, subnets, Cloud NAT, APIs) | `terraform/foundation` | [docs/gem-networking.md](docs/gem-networking.md) |
| Admin Workstation (`gem-admin-ws`, runs `bmctl`) | `terraform/admin-workstation`, `ansible/roles/workstation` | [docs/admin-workstation.md](docs/admin-workstation.md) |
| GEM Clusters (3-node GDC-like environments) | `terraform/cluster`, `ansible/roles/{cluster_nodes,gdc_deploy,gvisor,gvisor_node}` | [docs/project-setup.md](docs/project-setup.md) |
| Edge Router (Traefik proxy to MetalLB VIPs) | `terraform/edge-router`, `ansible/roles/edge_router` | [docs/edge-router.md](docs/edge-router.md) |
| Networking & VXLAN overlay | `ansible/roles/{vxlan,secondary_networks}` | [docs/gem-networking.md](docs/gem-networking.md) |
| Secondary Networks & Multi-Network Gateway API (`gem-network-operator`) | `operators/gem-network-operator`, `ansible/roles/secondary_networks` | [docs/secondary-networks.md](docs/secondary-networks.md), [docs/gem-network-operator-implementation.md](docs/gem-network-operator-implementation.md), [docs/gem-network-operator-remediation.md](docs/gem-network-operator-remediation.md) |
| Storage (TopoLVM + Gatekeeper mutations) | `ansible/roles/{topolvm,gatekeeper}`, `policies/storage` | [docs/storage.md](docs/storage.md) |
| Cloud Build pipelines | `cloudbuild/`, `terraform/cloudbuild` | [docs/cloud-build.md](docs/cloud-build.md) |
| Project / GCP setup | `project-setup.sh` | [docs/project-setup.md](docs/project-setup.md) |

## ⚠️ Constraints & Gotchas
These are the cross-cutting rules that an agent must not break. They are intentionally kept in this always-loaded file; the deep-dive docs explain the reasoning.

Networking (see [docs/gem-networking.md](docs/gem-networking.md)):

*   **MTU is 1410**: GCP VPC caps MTU at 1460 and VXLAN adds 50 bytes of overhead, so all overlay interfaces **must** use an MTU of **1410**. TCP MSS clamping on the primary VXLAN interface is required, or large TLS payloads are silently dropped (mysterious handshake freezes).
*   **Interface naming is load-bearing**: Nodes use `vxlan0` and `gdcenet0.<vlan_id>` so unmodified GDC `Network` CRs discover them. Shared hosts (workstation, edge router) use `vx-<truncated_cluster>-<vni>` and `sec-<truncated_cluster>-<vlan_id>`. Do not rename without updating every consumer.
*   **Hostname Assumption**: The VXLAN scripts assume hostnames end in a number (e.g., `node1`) to derive IP octets. Do not change node naming without updating the scripts.
*   **`gem-network-operator` runs per-cluster on the shared Admin Workstation**: Multiple clusters' operator instances coexist on one host, so the systemd unit disables metrics/health-probe binding (`--metrics-bind-address=0 --health-probe-bind-address=0`) to avoid port collisions. Do not re-enable these without giving each cluster's instance a unique port.
*   **Don't touch `clusterdns-controller` when editing CoreDNS for Gateway API**: `gem-network-operator` and the pre-existing `clusterdns-controller`/`clusterdns-webhook` both reconcile the `coredns-config` ConfigMap. Scaling `clusterdns-controller` to zero to avoid the race (previously tried) breaks it; the fix was to leave it running and just append the `.gkegw.cluster.local` rewrite rule idempotently. See [docs/secondary-networks.md](docs/secondary-networks.md).

Storage (see [docs/storage.md](docs/storage.md)):

*   **Gatekeeper mutations are the emulation**: Unmodified GDC manifests request `robin` StorageClasses and RWX volumes. Gatekeeper rewrites `robin` → `topolvm.io`, forces `WaitForFirstConsumer`, and downgrades `ReadWriteMany` → `ReadWriteOnce` (TopoLVM is RWO-only). Keep these in sync if you touch storage emulation.
*   **Partition boundary coupling**: `node_storage_size` (default `100GB`) is the split point between the `node_storage` partition created by Terraform cloud-init (`terraform/cluster/cluster-nodes.tf`) and the TopoLVM partition created by the Ansible `cluster_nodes` role. The two are defined in different tools and **must** agree, or you get a gap or overlap on the disk.

Platform & Tooling:

*   **Cluster Name Length & Kubernetes 63-char Hostname Limit**: GCE VM FQDNs take the form `<cluster_name>-<node>.<zone>.c.<project_id>.internal`. Kubernetes node registration fails if the FQDN exceeds **63 characters** (`metadata.labels: Invalid value: must be no more than 63 characters`), causing `bmctl` node pool timeouts. Cluster name length must satisfy `len(cluster_name) <= 63 - len(zone) - len(project_id) - 15` (typically $\le 16$ characters in longer GCP projects).
*   **Prefer Native Declarations**: Always use native declarative operations and features provided by the tooling (such as Kyverno Chainsaw `patch`/`apply`/`assert` operations, Terraform resources, or Ansible modules) rather than shelling out to raw scripts or CLI commands (`kubectl`, `gcloud`, etc.) whenever possible.
*   **QEMU Fallback**: If the GCP project enforces Shielded VMs (Secure Boot), nested virtualization (KVM) fails and the cluster falls back to QEMU software emulation, which breaks Windows VM scheduling (no Hyper-V features). Workaround: set `osType: Linux` for all VMs when using QEMU.
*   **Storage capacity vs. usable emulation**: The cluster provides ~3.9 TB aggregate raw storage (~1.3 TB per node via TopoLVM). Real GDC with Robin SDS typically yields only ~1.3 TB usable due to 3-way replication. GEM does not enforce this lower limit, to allow testing larger single volumes.

## 🚀 Key Workflows
*   **Dynamic Inventory**: `ansible/inventory.sh` reads state directly from **GCS** (`gs://gem-${PROJECT_ID}-tfstate/...`) rather than local Terraform state files. `PROJECT_ID` must be set in your environment or available via `gcloud`.
*   **Automated SSH Key Gen**: `ansible/create-cluster.yaml` runs a local play that generates `~/.ssh/google_compute_engine` via `gcloud compute config-ssh --quiet` when it is missing.
*   **Device Readiness**: Tasks that wait for device files in `/dev` use `udevadm settle` rather than fixed `sleep` delays, in both cloud-init and Ansible.
*   **Dynamic Version Mapping**: `emulate_gdc_version` maps a GDC version to a specific Anthos Bare Metal version, enabling multi-version testing.
*   **Multi-Version `bmctl`**: The workstation keeps multiple `bmctl` binaries in `/usr/local/sbin/` and symlinks the active one based on the requested version.
*   **Idempotent VXLAN**: The VXLAN service task uses `state: started` to avoid interface flapping on repeated playbook runs.

## 🗺️ Future Work
*   **Gateway VIP allocation is not real allocation**: `gem-network-operator` always assigns the first host address of the matching `GKEGatewayCIDR` rather than tracking issued IPs. Multiple `Gateway`s pointed at the same CIDR will collide (behavior pinned by `TestGatewayReconciler_Reconcile_TwoGatewaysSameCIDRCollide`). Fine for the current one-Gateway-per-network test topology; needs real IPAM before supporting more.
*   **Mutating admission webhook translates GDC pod interfaces to Multus**: `gem-network-operator` runs a mutating admission webhook that intercepts Pod creation, extracts secondary networks from `networking.gke.io/interfaces`, injects `k8s.v1.cni.cncf.io/networks` for Multus MACVLAN, and sanitizes `networking.gke.io/interfaces` so Cilium only configures the primary network interface. Pod routing continues to be handled via `host-local` IPAM gateway injection at pod-init time.
*   **Secondary-network pod IPAM can collide with node host IPs**: the `NetworkAttachmentDefinition`s use `host-local` IPAM over the full secondary subnet with only the gateway excluded, and each node allocates independently. Pods can therefore receive the nodes' own `gdcenet0.<vlan>` host IPs (`.2`/`.3`/`.4`) or duplicate a pod IP on another node. Observed live: a pod was assigned `192.168.45.4` — a node's host IP — and traffic still flowed only thanks to macvlan bridge-mode isolation. Add per-node disjoint `rangeStart`/`rangeEnd` (or exclusions for host IPs) before scaling out secondary-network workloads.
*   **Secondary-network VNI derivation is list-order-dependent**: `ansible/roles/vxlan` computes each secondary network's VNI as `vxlan_id + loop.index` — the VLAN ID never enters the calculation. Reordering, inserting, or removing an entry in `secondary_networks` (`ansible/group_vars/all.yaml`) and re-running the `vxlan` role against a live cluster silently reassigns VNIs for unrelated networks, breaking cross-node traffic for them. Derive the VNI from the VLAN ID (or a name hash) instead of list position before allowing any post-build re-run of that role.
