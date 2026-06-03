# GEM Storage Design

This document describes how GEM emulates the storage behavior of a physical Google Distributed Cloud (GDC) Connected environment, including the on-disk layout, the TopoLVM provisioner, and the Gatekeeper mutations used to translate GDC storage configurations without modifying workload manifests.

## Storage Overview

A physical GDC Connected environment uses Symcloud Storage (formerly Robin) as its default software-defined storage (SDS) provider, exposing `robin` StorageClasses with `ReadWriteMany` (RWX) support backed by 3-way replication. GEM does not run Robin. Instead, it provisions storage with [TopoLVM](https://github.com/topolvm/topolvm), an LVM-backed CSI driver that allocates logical volumes from a node-local volume group.

Because a requirement of GEM is to apply unmodified GDC workload manifests and observe identical behavior, the gap between Robin and TopoLVM is bridged with OPA Gatekeeper mutations rather than by editing manifests. Workloads continue to request `robin` StorageClasses and RWX volumes; the mutations rewrite those requests into the equivalent TopoLVM-compatible form at admission time.

## On-Disk Layout

Each cluster node is provisioned with a dedicated 1400 GB `pd-ssd` data disk (`terraform/cluster/cluster-nodes.tf`) attached as `/dev/disk/by-id/google-data`. The disk is split into two partitions at a boundary controlled by the `node_storage_size` variable (default `100GB`):

| Partition | Range | Created by | Purpose |
| :--- | :--- | :--- | :--- |
| `node_storage` (part1) | `0%` → `node_storage_size` | Terraform cloud-init (`cluster-nodes.tf`) | ext4 filesystem mounted at `/mnt/node_storage` for node-local files. |
| `topolvm` (part2) | `node_storage_size` → `100%` | Ansible `cluster_nodes` role | Physical volume added to the `topolvm-vg` volume group consumed by TopoLVM. |

The two partitions are defined in different tools but must meet exactly at `node_storage_size`. See the coupling note in [AGENTS.md](../AGENTS.md) before changing either value.

## TopoLVM Provisioner

TopoLVM is deployed via Helm into the `topolvm-system` namespace by the `topolvm` Ansible role (`ansible/roles/topolvm`). Its configuration (`topolvm-values.yaml.j2`) establishes a single default device class, `gdc-storage`, backed by the `topolvm-vg` volume group with a 10 GB spare reservation.

Two non-default settings are deliberate and load-bearing:

*   **No default StorageClass** (`storageClasses: []`): GDC-style `robin` StorageClasses are supplied by workloads and mutated to use the `topolvm.io` provisioner, so TopoLVM must not create a competing default.
*   **Pod mutating webhook disabled** (`webhook.podMutatingWebhook.enabled: false`): TopoLVM normally injects a synthetic `topolvm.io/capacity` resource request to steer the scheduler toward nodes with free capacity. Under Anthos Bare Metal with containerd, the device plugin fails to advertise that capacity to the kubelet, leaving pods stuck `Pending` with `FailedScheduling`. Disabling the webhook lets native `WaitForFirstConsumer` binding handle placement instead.

## Gatekeeper Storage Mutations

The mutations live in `policies/storage/` and translate Robin semantics to TopoLVM at admission time:

*   **Provisioner translation** (`sc-robin-emulation.yaml`): rewrites the `provisioner` of any `*robin*` StorageClass to `topolvm.io`.
*   **Binding mode** (`force-wait-for-first-consumer.yaml`): forces `*robin*` StorageClasses to `volumeBindingMode: WaitForFirstConsumer`. Robin may use `Immediate` binding, which lets TopoLVM provision a workload's PVCs across different nodes simultaneously and deadlocks scheduling; delaying provisioning until the consuming pod is scheduled keeps all of a pod's volumes on one node.
*   **Access mode downgrade** (`rwx-mutation.yaml`): prunes `ReadWriteMany` from PVC access modes and merges in `ReadWriteOnce`, because TopoLVM only supports RWO.

## Capacity Caveat

TopoLVM exposes roughly 1.3 TB of usable storage per node (about 3.9 TB aggregate across the three-node cluster). A real GDC cluster running Robin SDS typically yields only ~1.3 TB of usable space in total due to 3-way replication. GEM does not currently enforce this lower effective limit, which allows testing of larger single volumes up to the per-node capacity. Keep this difference in mind when validating capacity-sensitive behavior.
