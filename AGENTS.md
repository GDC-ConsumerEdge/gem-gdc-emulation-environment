# Agent Guide: GEM (GDC EMulation Environment)

This file provides essential context, architectural details, and conventions for
AI agents working on the GEM project. Read this file to quickly and accurately
understand the project state and constraints.

## Project Goal

GEM emulates a physical Google Distributed Cloud (GDC) Connected environment
entirely on Google Compute Engine (GCE) instances. It is used for rapid
prototyping, testing, and validation of GDC workloads without requiring physical
hardware.

## Project Type and Tooling

GEM is an infrastructure-as-code project, not an application codebase. It is
built from Terraform (`terraform/`), Ansible (`ansible/`), Bash (`scripts/`,
`cloudbuild/`), and Cloud Build pipelines.

Validate changes locally without provisioning real infrastructure:

- `pre-commit run --all-files`: formatting, linting, license headers, and secret
  scanning.
- `./scripts/run-unit-tests.sh`: Terraform `terraform test` plus Ansible check
  playbooks.

Never run `terraform apply` or live Ansible plays against real GCP resources to
validate a change unless approved. Both operations create or mutate live
infrastructure.

## Task Playbooks

Step-by-step procedures for common changes live in [skills/](skills/). Read the
relevant one before making that kind of change:

- [skills/validate.md](skills/validate.md): validate a change locally without
  provisioning infrastructure.
- [skills/technical-writer.md](skills/technical-writer.md): write or edit any
  documentation, including `README.md`, `docs/`, and role or module READMEs.
  Read this before editing a single Markdown file. It defines the project's
  house style, which is specific and non-obvious.
- [skills/add-ansible-role.md](skills/add-ansible-role.md): scaffold and wire a
  new Ansible role.
- [skills/add-terraform-module.md](skills/add-terraform-module.md): scaffold a
  new Terraform module.

## Documentation Links

If you are unsure about how a specific technology works or have questions about
its usage in this project, refer to the official documentation links below:

- **Google Distributed Cloud Connected**:
  [GDC Connected Docs](https://docs.cloud.google.com/distributed-cloud/connected/latest/docs/overview)
- **Google Distributed Cloud (Software-only) for bare metal**:
  [GDC Bare Metal Docs](https://docs.cloud.google.com/kubernetes-engine/distributed-cloud/bare-metal/docs/concepts/about-bare-metal)
- **Google Compute Engine (GCE)**:
  [GCE Docs](https://docs.cloud.google.com/compute/docs/overview)
- **Terraform**:
  [Terraform Docs](https://developer.hashicorp.com/terraform/docs)
- **Ansible**: [Ansible Docs](https://docs.ansible.com/)
- **Kyverno Chainsaw** (Testing):
  [Chainsaw Docs](https://kyverno.github.io/chainsaw/)
- **OPA Gatekeeper** (Policy Enforcement):
  [Gatekeeper Docs](https://open-policy-agent.github.io/gatekeeper/website/docs/)
- **TopoLVM** (Storage): [TopoLVM Docs](https://github.com/topolvm/topolvm)
- **Traefik** (Edge Router): [Traefik Docs](https://doc.traefik.io/traefik/)

## Component Map

GEM is built from independent components, each provisioned by Terraform and
configured by Ansible. Start here to find the right code and the deep-dive doc
for the area you are changing.

| Component                                                                 | Primary code                                                                       | Deep-dive doc                                                                                                                                        |
| :------------------------------------------------------------------------ | :--------------------------------------------------------------------------------- | :--------------------------------------------------------------------------------------------------------------------------------------------------- |
| Foundation (VPC `gem-clusters-vpc`, subnets, Cloud NAT, APIs)             | `terraform/foundation`                                                             | [docs/gem-networking.md](docs/gem-networking.md)                                                                                                     |
| Admin Workstation (`gem-admin-ws`, runs `bmctl`)                          | `terraform/admin-workstation`, `ansible/roles/workstation`                         | [docs/admin-workstation.md](docs/admin-workstation.md)                                                                                               |
| GEM Clusters (3-node GDC-like environments)                               | `terraform/cluster`, `ansible/roles/{cluster_nodes,gdc_deploy,gvisor,gvisor_node}` | [docs/project-setup.md](docs/project-setup.md)                                                                                                       |
| Edge Router (Traefik proxy to MetalLB VIPs)                               | `terraform/edge-router`, `ansible/roles/edge_router`                               | [docs/edge-router.md](docs/edge-router.md)                                                                                                           |
| Networking and VXLAN overlay                                              | `ansible/roles/{vxlan,secondary_networks}`                                         | [docs/gem-networking.md](docs/gem-networking.md)                                                                                                     |
| Secondary Networks and Multi-Network Gateway API (`gem-network-operator`) | `operators/gem-network-operator`, `ansible/roles/secondary_networks`               | [docs/secondary-networks.md](docs/secondary-networks.md), [docs/gem-network-operator-implementation.md](docs/gem-network-operator-implementation.md) |
| Storage (TopoLVM + Gatekeeper mutations)                                  | `ansible/roles/{topolvm,gatekeeper}`, `policies/storage`                           | [docs/storage.md](docs/storage.md)                                                                                                                   |
| Cloud Build pipelines                                                     | `cloudbuild/`, `terraform/cloudbuild`                                              | [docs/cloud-build.md](docs/cloud-build.md)                                                                                                           |
| GEM REST API (FastAPI orchestration service)                              | `api/`                                                                             | [docs/gem-api.md](docs/gem-api.md)                                                                                                                   |
| Project / GCP setup                                                       | `project-setup.sh`                                                                 | [docs/project-setup.md](docs/project-setup.md)                                                                                                       |

## Constraints and Gotchas

These are the cross-cutting rules that an agent must not break. They are
intentionally kept in this always-loaded file; the deep-dive docs explain the
reasoning.

Networking (see [docs/gem-networking.md](docs/gem-networking.md)):

- **MTU is 1410**: GCP VPC caps MTU at 1460 and VXLAN adds 50 bytes of overhead,
  so all overlay interfaces **must** use an MTU of **1410**. TCP MSS clamping on
  the primary VXLAN interface is required, or large TLS payloads are silently
  dropped (mysterious handshake freezes).
- **Interface naming is load-bearing**: Nodes use `vxlan0` and
  `gdcenet0.<vlan_id>` so unmodified GDC `Network` CRs discover them. Shared
  hosts (workstation, edge router) use `vx-<truncated_cluster>-<vni>` and
  `sec-<truncated_cluster>-<vlan_id>`. Do not rename without updating every
  consumer.
- **Hostname Assumption**: The VXLAN scripts assume hostnames end in a number
  (e.g., `node1`) to derive IP octets. Do not change node naming without
  updating the scripts.
- **`gem-network-operator` runs per-cluster on the shared Admin Workstation**:
  Multiple clusters' operator instances coexist on one host, so the systemd unit
  disables metrics/health-probe binding
  (`--metrics-bind-address=0 --health-probe-bind-address=0`) to avoid port
  collisions. Do not re-enable these without giving each cluster's instance a
  unique port.
- **Don't touch `clusterdns-controller` when editing CoreDNS for Gateway API**:
  `gem-network-operator` and the pre-existing
  `clusterdns-controller`/`clusterdns-webhook` both reconcile the
  `coredns-config` ConfigMap. Scaling `clusterdns-controller` to zero to avoid
  the race (previously tried) breaks it; the fix was to leave it running and
  just append the `.gkegw.cluster.local` rewrite rule idempotently. See
  [docs/secondary-networks.md](docs/secondary-networks.md).

Storage (see [docs/storage.md](docs/storage.md)):

- **Gatekeeper mutations are the emulation**: Unmodified GDC manifests request
  `robin` StorageClasses and RWX volumes. Gatekeeper rewrites `robin` →
  `topolvm.io`, forces `WaitForFirstConsumer`, and downgrades `ReadWriteMany` →
  `ReadWriteOnce` (TopoLVM is RWO-only). Keep these in sync if you touch storage
  emulation.
- **Partition boundary coupling**: `node_storage_size` (default `100GB`) is the
  split point between the `node_storage` partition created by Terraform
  cloud-init (`terraform/cluster/cluster-nodes.tf`) and the TopoLVM partition
  created by the Ansible `cluster_nodes` role. The two are defined in different
  tools and **must** agree, or you get a gap or overlap on the disk.

Documentation (see [skills/technical-writer.md](skills/technical-writer.md) for
the full style):

- **The inclusion test**: document how to accomplish a task and why it works,
  including configuration options, then stop. Implementation detail belongs in
  the code, which is the source of truth. Restating it in prose guarantees
  drift. This is not a length rule, a long document is fine if every part of it
  passes.
- **Point at the source, never reproduce it**: never restate in prose what a
  config file, Dockerfile, manifest or module already declares. Link it. Pinned
  versions, permission lists and copied-out variable tables are the usual
  offenders, and `file:line` citations are the worst of them. This file is the
  only place line-level citations are acceptable, because the audience here is
  an agent that is about to read the code anyway.

Platform and Tooling:

- **Cluster Name Length and Kubernetes 63-char Hostname Limit**: GCE VM FQDNs
  take the form `<cluster_name>-<node>.<zone>.c.<project_id>.internal`.
  Kubernetes node registration fails if the FQDN exceeds **63 characters**
  (`metadata.labels: Invalid value: must be no more than 63 characters`),
  causing `bmctl` node pool timeouts. Cluster name length must satisfy
  `len(cluster_name) <= 63 - len(zone) - len(project_id) - 15` (typically $\\le
  16$ characters in longer GCP projects).
- **Prefer Native Declarations**: Always use native declarative operations and
  features provided by the tooling (such as Kyverno Chainsaw
  `patch`/`apply`/`assert` operations, Terraform resources, or Ansible modules)
  rather than shelling out to raw scripts or CLI commands (`kubectl`, `gcloud`,
  etc.) whenever possible.
- **Hardware virtualization is declared, not best-effort**:
  `terraform/cluster/cluster-nodes.tf` sets
  `advanced_machine_features.enable_nested_virtualization = true` and
  `shielded_instance_config.enable_secure_boot = false` on every node, and
  `terraform/tests/unit.tftest.hcl` asserts the former. There is no QEMU
  software-emulation fallback path: in a project that enforces
  `constraints/compute.requireShieldedVm`, `terraform apply` fails on the policy
  instead. Do not weaken either setting.
- **Storage capacity vs. usable emulation**: The cluster provides ~3.9 TB
  aggregate raw storage (~1.3 TB per node via TopoLVM). Real GDC with Robin SDS
  typically yields only ~1.3 TB usable due to 3-way replication. GEM does not
  enforce this lower limit, to allow testing larger single volumes.

## Key Workflows

- **Dynamic Inventory**: `ansible/inventory.sh` reads state directly from
  **GCS** (`gs://gem-${PROJECT_ID}-tfstate/...`) rather than local Terraform
  state files. `PROJECT_ID` must be set in your environment or available via
  `gcloud`.
- **Automated SSH Key Gen**: `ansible/create-cluster.yaml` runs a local play
  that generates `~/.ssh/google_compute_engine` via
  `gcloud compute config-ssh --quiet` when it is missing.
- **Device Readiness**: Tasks that wait for device files in `/dev` use
  `udevadm settle` rather than fixed `sleep` delays, in both cloud-init and
  Ansible.
- **Dynamic Version Mapping**: `emulate_gdc_version` maps a GDC version to a
  specific Anthos Bare Metal version, enabling multi-version testing.
- **Multi-Version `bmctl`**: The workstation keeps multiple `bmctl` binaries in
  `/usr/local/sbin/` and symlinks the active one based on the requested version.
- **Idempotent VXLAN**: The VXLAN service task uses `state: started` to avoid
  interface flapping on repeated playbook runs.

## Future Work

- **Gateway VIP allocation is not real allocation**: `gem-network-operator`
  always assigns the first host address of the matching `GKEGatewayCIDR` rather
  than tracking issued IPs. Multiple `Gateway`s pointed at the same CIDR will
  collide (behavior pinned by
  `TestGatewayReconciler_Reconcile_TwoGatewaysSameCIDRCollide`). Fine for the
  current one-Gateway-per-network test topology; needs real IPAM before
  supporting more.
- **Mutating admission webhook translates GDC pod interfaces to Multus**:
  `gem-network-operator` runs a mutating admission webhook that intercepts Pod
  creation, extracts secondary networks from `networking.gke.io/interfaces`,
  injects `k8s.v1.cni.cncf.io/networks` for Multus MACVLAN, and sanitizes
  `networking.gke.io/interfaces` so Cilium only configures the primary network
  interface. Pod routing continues to be handled via `host-local` IPAM gateway
  injection at pod-init time.
- **Secondary-network pod IPAM can collide with node host IPs**: the
  `NetworkAttachmentDefinition`s use `host-local` IPAM over the full secondary
  subnet with only the gateway excluded, and each node allocates independently.
  Pods can therefore receive the nodes' own `gdcenet0.<vlan>` host IPs
  (`.2`/`.3`/`.4`) or duplicate a pod IP on another node. Observed live: a pod
  was assigned `192.168.45.4` (a node's host IP) and traffic still flowed only
  thanks to macvlan bridge-mode isolation. Add per-node disjoint
  `rangeStart`/`rangeEnd` (or exclusions for host IPs) before scaling out
  secondary-network workloads.
- **Secondary-network VNI derivation is list-order-dependent**:
  `ansible/roles/vxlan` computes each secondary network's VNI as
  `vxlan_id + loop.index`. The VLAN ID never enters the calculation. Reordering,
  inserting, or removing an entry in `secondary_networks`
  (`ansible/group_vars/all.yaml`) and re-running the `vxlan` role against a live
  cluster silently reassigns VNIs for unrelated networks, breaking cross-node
  traffic for them. Derive the VNI from the VLAN ID (or a name hash) instead of
  list position before allowing any post-build re-run of that role.
- **Traefik on the edge router has no routes and is not in the data path**:
  `ansible/roles/workstation/tasks/traefik.yaml` creates `/etc/traefik/dynamic/`
  and nothing in the repo ever writes into it. There are no TCP/UDP entrypoints,
  no TLS configuration, and no provider that could discover MetalLB VIPs, so
  `:80` and `:443` return 404. Developer access is `scripts/gem-tunnel.sh`,
  which is OpenSSH `-L` forwarding through the VM's `sshd` over IAP. Do not
  describe Traefik as the ingress path, and do not assume a dynamic config
  exists.
- **Shared-host secondary interface names omit the VNI**: nodes get
  `gdcenet0.<vlan_id>`, but the workstation and edge router get
  `sec-<cluster6>-<vlan_id>` with no VNI component. Two clusters whose first six
  alphanumeric characters match (`gem-cluster-1` and `gem-cluster-2` both
  truncate to `gemclu`) collide on the same interface name and the same gateway
  IP, and `ansible/cleanup.yaml`'s `*<cluster6>*` globs delete the sibling
  cluster's config from GCS and `/etc/systemd/network`. Include the VNI in the
  `sec-*` name before supporting same-prefix cluster names.
- **`gem-network-operator` unit resolves `--webhook-host` through a host key
  that does not exist**: the template reads
  `hostvars['admin_workstation_host']`, but `ansible/inventory.sh` names the
  host `gem_admin_ws`, so the expression always falls through to the literal
  `10.10.0.2`. That matches the Terraform default for `workstation_ip` today,
  which is why it works. Changing `workstation_ip` silently breaks the pod
  mutating webhook. Fix the lookup before making the workstation IP
  configurable.
- **Operator webhook port is hashed, not allocated**:
  `9443 + md5(cluster_name) % 500` can collide across clusters sharing the
  workstation, which is the same class of bug that metrics and health probes
  were disabled to avoid. Allocate explicitly.
- **Every cluster's secondary networks claim the same gateway address on the
  edge router**: `sec_ip` comes from the global `secondary_networks` list, and
  the edge-router branch of `ansible/roles/vxlan/tasks/main.yaml` assigns
  `net.gateway` verbatim. Two clusters with entirely different names therefore
  produce two interfaces (`sec-alpha1-123`, `sec-bravo2-123`) both configured
  `172.16.12.1/24`: a duplicate address and an overlapping route on one host.
  This is independent of the `cluster6` name collision above. Secondary networks
  are effectively single-cluster until gateways are allocated per cluster.
- **`terraform/admin-workstation` drifts away the Ansible-published SSH key**:
  `google_compute_instance.admin_ws` has no
  `lifecycle { ignore_changes = [metadata] }`, so a later `terraform apply`
  prunes the out-of-band `workstation_pubkey` metadata that
  `terraform/cluster/cluster-nodes.tf` reads. Cluster nodes created afterwards
  get `ssh-keys = "gem:"` and are unreachable. Re-running
  `admin-workstation.yaml` republishes it.
- **The GEM REST API has no authentication and holds state in memory**: no auth
  dependency exists on any route, CORS allows all origins with credentials, and
  operation records live in a process-local dict keyed by resource name, so the
  service is single-instance only and re-running an operation overwrites the
  previous record and its log file. It also shares the `clusters/<name>/state`
  Terraform prefix with Cloud Build, so the two paths contend on the same lock.
  Add authentication and durable state before exposing it beyond a trusted
  network.
