# GEM Storage

Workloads on a GEM cluster request storage exactly as they would on GDC
Connected: through a `robin` StorageClass. The storage volumes are really
provided by [TopoLVM](https://github.com/topolvm/topolvm), which provision LVM
logical volumes out of a data disk on each node. OPA Gatekeeper mutations
rewrite the GDC requests into a form TopoLVM accepts, so the manifests
themselves never change.

That translation keeps manifests portable, but it does not make the storage
behave like Robin. A TopoLVM volume lives on one node, with no replication and
no shared access. Read [Differences from GDC](#differences-from-gdc) before
testing anything that depends on failover, shared volumes or capacity.

## Why this exists

GDC Connected ships Symcloud Storage (formerly Robin) as its software-defined
storage. It exposes `robin` StorageClasses, and has the ability to replicate
data between cluster nodes if requested.

A core tenant of GEM is that unmodified GDC manifests apply cleanly and behave
the same way as they would on a GDC cluster. Editing manifests to name a
different provisioner would break that requirement, so the translation happens
at admission time instead. Workloads keep asking for Robin, and Gatekeeper
answers on TopoLVM's behalf.

## Requesting storage

Nothing in GEM creates a `robin` StorageClass, so the admission webhook mutates
any StorageClass whose name contains `robin`:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: robin
provisioner: robin # rewritten to topolvm.io on admission
parameters:
  csi.storage.k8s.io/fstype: ext4
allowVolumeExpansion: true
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data
spec:
  storageClassName: robin
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 10Gi
```

To confirm the translation took effect:

```bash
# Prints topolvm.io, not robin
kubectl get storageclass robin -o jsonpath='{.provisioner}{"\n"}'

# Shows ReadWriteOnce
kubectl get pvc data -o jsonpath='{.spec.accessModes}{"\n"}'

# On the node running the pod, lists one logical volume per bound claim
sudo lvs topolvm-vg
```

## How it works

Three layers produce a volume: Terraform and Ansible split each node's data disk
into partitions, TopoLVM allocates volumes from one of them, and Gatekeeper
steers `robin` requests to TopoLVM.

### Disk layout

Every cluster node has a dedicated data disk, attached as
`/dev/disk/by-id/google-data`. Its size and type come from the cluster's
`hardware_variant`, defined in
[hardware-variants.tf](../terraform/cluster/hardware-variants.tf). The disk is
split into two partitions at the `node_storage_size` boundary, `100GB` by
default:

| Partition              | Range                        | Created by                                                                 | Purpose                                                         |
| :--------------------- | :--------------------------- | :------------------------------------------------------------------------- | :-------------------------------------------------------------- |
| `node_storage` (part1) | `0%` → `node_storage_size`   | cloud-init, from [cluster-nodes.tf](../terraform/cluster/cluster-nodes.tf) | ext4, mounted at `/mnt/node_storage`. No GEM component uses it. |
| `topolvm` (part2)      | `node_storage_size` → `100%` | The [`cluster_nodes`](../ansible/roles/cluster_nodes/) Ansible role        | The sole physical volume in the `topolvm-vg` volume group.      |

Both steps check for an existing partition before creating one, so neither
repartitions a disk that is already set up.

### TopoLVM

The [`topolvm`](../ansible/roles/topolvm/) Ansible role installs TopoLVM with
Helm into the `topolvm-system` namespace. Its
[values file](../ansible/roles/topolvm/templates/topolvm-values.yaml.j2) defines
a single device class backed by `topolvm-vg`, and departs from the chart
defaults in two places:

- **It creates no StorageClasses.** The `robin` StorageClass a workload defines
  is meant to be the only route to TopoLVM, so the chart's own class is
  disabled.
- **The pod mutating webhook is off.** TopoLVM normally adds a synthetic
  `topolvm.io/capacity` resource request to each pod so the scheduler favors
  nodes with free space. On Anthos Bare Metal with containerd, the device plugin
  never advertises that resource to the kubelet, and every pod that uses a
  volume sits `Pending` with `FailedScheduling`. `WaitForFirstConsumer` binding
  handles placement instead.

### Gatekeeper mutations

The mutations live in [policies/storage/](../policies/storage/) and are applied
by the [`gatekeeper`](../ansible/roles/gatekeeper/) Ansible role along with the
rest of `policies/`:

- **Provisioner.** Any StorageClass whose name contains `robin` has its
  `provisioner` set to `topolvm.io`.
- **Binding mode.** The same StorageClasses are forced to
  `volumeBindingMode: WaitForFirstConsumer`. With `Immediate` binding, TopoLVM
  would create each of a pod's volumes as soon as its claim appeared, possibly
  on different nodes. A pod that needs volumes on two nodes can never be
  scheduled.
- **Access mode.** `ReadWriteMany` is removed from a claim's access modes and
  `ReadWriteOnce` is added, because a TopoLVM volume lives on one node and
  cannot be mounted from any other. Unlike the other two, this mutation matches
  **every PVC in the cluster**, whatever its StorageClass.

## Differences from GDC

The mutations make GDC manifests apply cleanly. They do not reproduce Robin's
behavior, and each difference below changes what a test on GEM can tell you.

- **Volumes are pinned to one node.** A TopoLVM volume is a logical volume on a
  single node's disk, so a pod using it can only ever run on that node. If the
  node is drained, the pod cannot move. If the node is lost, so is the data. On
  GDC, Robin's replicas let the pod restart elsewhere. Failover, drain and
  node-loss testing on GEM does not reflect GDC.

- **Capacity is larger than on GDC.** Each node offers roughly its data disk
  size, minus `node_storage_size`. Robin's three-way replication means a real
  cluster offers about a third of its raw capacity. GEM does not enforce that
  lower limit, which lets you test volumes larger than GDC could provision, but
  capacity-sensitive behavior will differ. The reverse also applies on small
  variants: `node_storage_size` is a fixed amount, so `dev-and-test`, with a 150
  GB data disk, leaves only about 40 GB per node for volumes.

- **Other StorageClasses exist.** Anthos Bare Metal creates its own local volume
  classes, `node-disk` and `local-shared`, configured in the
  [cluster template](../ansible/roles/gdc_deploy/templates/cluster.yaml.j2).
  They are backed by paths on the boot disk, not the data disk, and sit outside
  the emulation entirely.

## Changing the partition boundary

`node_storage_size` is a variable of the [cluster](../terraform/cluster/)
Terraform module. Set it in the module's `terraform.tfvars`, or on the command
line when you build the cluster:

```bash
terraform apply -var="cluster_name=${CLUSTER_NAME}" -var="node_storage_size=50GB"
```

Set it only in Terraform. The Ansible inventory reads the value back from
Terraform state, so both partitions always use the same boundary. Passing
`node_storage_size` to `ansible-playbook` would override that value and leave a
gap or an overlap on the disk.

The value only affects nodes when they are first built. Both partitioning steps
skip a disk that is already partitioned, so changing it on an existing cluster
does nothing until the cluster is rebuilt. Neither the
[Cloud Build pipelines](cloud-build.md) nor the [GEM REST API](gem-api.md) pass
this variable, so clusters built through them always use the default.

## Related documentation

- [README: GDC Hardware Configurations](../README.md#gdc-hardware-configurations)
  lists the hardware variants and how to choose one.
- [TopoLVM documentation](https://github.com/topolvm/topolvm/tree/main/docs)
  covers device classes, capacity-aware scheduling and the Helm chart.
- [Gatekeeper mutation](https://open-policy-agent.github.io/gatekeeper/website/docs/mutation/)
  documents the `Assign` and `ModifySet` mutators used here.
