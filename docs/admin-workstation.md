# GEM Admin Workstation

The GEM Admin Workstation (`gem-admin-ws`) is the GCE instance that builds and
manages every GEM cluster in a project. It runs `bmctl`, holds each cluster's
kubeconfig, hosts the per-cluster `gem-network-operator` daemons, and
participates in every cluster's VXLAN overlay.

It is a shared, long-lived host. One admin workstation serves every cluster in
the project.

## Why this exists

Creating an Anthos Bare Metal cluster with `bmctl` requires a local KinD
bootstrap cluster running in Docker on the installer machine. That machine needs
Docker installed and configured, direct network connectivity to the cluster
nodes, and a service account key on disk.

Putting the installer on a dedicated GCE instance keeps those requirements off
your laptop, off the cluster nodes, and gives every cluster build the same
toolchain regardless of who triggers it. It also gives Cloud Build somewhere to
SSH into that already has the tooling and the required network connectivity.

## What runs on it

| Component                 | Purpose                                                    | Installed by                          |
| :------------------------ | :--------------------------------------------------------- | :------------------------------------ |
| `bmctl`                   | Creates and destroys GEM clusters                          | `workstation` role, then `gdc_deploy` |
| Docker                    | Hosts the KinD bootstrap cluster                           | `workstation` role                    |
| `kubectl`                 | Cluster access                                             | `workstation` role                    |
| Helm                      | Installs TopoLVM into clusters                             | `workstation` role                    |
| Go toolchain              | Builds `gem-network-operator` on the box                   | `workstation` role                    |
| `gem-network-operator`    | One systemd unit per cluster, built from source on the box | `secondary_networks` role             |
| VXLAN overlay interfaces  | Reachability to every cluster's nodes                      | `vxlan` role                          |
| Overlay synchronizer cron | Restores overlay config after a rebuild                    | `admin-workstation.yaml`              |

Gatekeeper is applied straight from an upstream manifest with `kubectl`, not
with Helm. The overlay synchronizer's task file lives under the `vxlan` role,
but the role does not install it, so running the role alone will not put it
back.

## Connecting

The instance has no external IP. SSH goes through Identity-Aware Proxy.

```bash
gcloud compute ssh gem@gem-admin-ws \
  --tunnel-through-iap \
  --project="${PROJECT_ID}" \
  --zone="${GEM_GCP_ZONE}"
```

Use the `gem` account for cluster work. It owns the bmctl workspace, the cluster
kubeconfigs, and the Anthos service account key, so anything touching those is
easiest as `gem`, either by connecting as above or with `sudo -iu gem`. OS Login
is disabled on the admin workstation instance.

Ansible connects differently. `ansible/inventory.sh` builds a
`gcloud compute start-iap-tunnel` ProxyCommand and sets `ansible_user` to your
own `$USER`, then escalates with `become: true`. The `gem` account is used only
where a task specifies `become_user`.

### Once connected to the admin workstation

Watch a cluster build in progress:

```bash
tail -f ~/bmctl-workspace/${CLUSTER_NAME}/log/create-cluster-*/create-cluster.log
```

Use a cluster's admin kubeconfig:

```bash
export KUBECONFIG=/home/gem/bmctl-workspace/${CLUSTER_NAME}/${CLUSTER_NAME}-kubeconfig
kubectl get nodes
```

> [!CAUTION]
> This is the cluster's break-glass cluster-admin credential. For routine
> access, use GKE Connect Gateway from your own machine instead, which is
> audited and respects IAM:
>
> ```bash
> gcloud config set auth/impersonate_service_account \
>   gem-cluster-admin@${PROJECT_ID}.iam.gserviceaccount.com
>
> gcloud container fleet memberships get-credentials ${CLUSTER_NAME} \
>   --project="${PROJECT_ID}"
> ```
>
> The impersonation is not optional. `gem-cluster-admin@` is the only identity
> with a `cluster-admin` binding, so without it you get a working kubeconfig
> that is authorized for nothing.

Check the per-cluster system daemons:

```bash
systemctl status gem-network-operator-${CLUSTER_NAME}
journalctl -u gem-network-operator-${CLUSTER_NAME} -n 100
systemctl status vxlan-tcpmss-${CLUSTER_NAME}
```

## Build the admin workstation

Build the admin workstation after the
[foundation environment setup](../README.md#environment-setup) and before
attempting to create any cluster.

### 1. Provision with Terraform

Terraform will build the infrastructure components required to run the admin
workstation:

```bash
cd terraform/admin-workstation

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=admin-workstation/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply
```

The default instance type is an `e2-standard-4` on Ubuntu 24.04 with a 50 GB
boot disk, no external IP, and `can_ip_forward` enabled for the overlay. Egress
goes through Cloud NAT.

### 2. Configure with Ansible

With the VM running, now run the Ansible admin workstation playbooks:

```bash
cd ansible
CLUSTER_NAME=none ansible-playbook admin-workstation.yaml
```

> [!NOTE]
> `CLUSTER_NAME=none` is required only because `ansible/inventory.sh` expects it
> and will error without it.

> [!WARNING]
> Every run of this playbook performs a full `apt dist-upgrade` and reboots the
> workstation if the upgrade leaves a `reboot-required` marker behind. The
> workstation is shared by every cluster in the project, so do not re-run it
> while a cluster build is in flight.

Four things the role does are worth knowing about, because other parts of the
system depend on them:

- It generates an RSA keypair at `/home/gem/.ssh/id_rsa` and publishes the
  public half to instance metadata as `workstation_pubkey`. `terraform/cluster`
  reads that metadata to authorize the `gem` user on cluster nodes, so the admin
  workstation must be configured before any cluster is built.
- It creates an Anthos service account key at `/home/gem/bm-gcr.json` and
  exports `GOOGLE_APPLICATION_CREDENTIALS` in `~/.bashrc`. `bmctl` needs this to
  pull images.
- It pins the Docker daemon to MTU 1410 and raises the inotify limits, both of
  which KinD and `bmctl` require.
- If the Cloud Build SSH secret already exists, it authorizes the key for
  inbound SSH and publishes the private key as a new Secret Manager version. The
  secret has to exist first, which is why the playbook is re-run after
  `cloudbuild/setup.sh`. See [Cloud Build](cloud-build.md).

## bmctl versions

Each GDC Connected version maps to a specific Anthos Bare Metal version. The map
is `emulated_gdc_versions` in
[`ansible/group_vars/all.yaml`](../ansible/group_vars/all.yaml).

Versions are fetched on demand rather than all at once. The `workstation` role
installs the default version at build time, and `gdc_deploy` downloads whatever
other version a cluster asks for when that cluster is built. Each lands at
`/usr/local/sbin/bmctl-<abm version>`, and `/usr/local/sbin/bmctl` is a symlink
to the version chosen at cluster build time. A freshly built workstation has
exactly one binary.

> [!WARNING]
> That `bmctl` symlink is a shared resource. The `gdc_deploy` role re-points it
> at the start of every cluster build, so it reflects whichever cluster was
> built most recently. Building two clusters on different GDC versions at the
> same time can flip the symlink mid-run. Build them one at a time.

## Networking role

The admin workstation joins every cluster's VXLAN overlay network so it can
reach the nodes that `bmctl` is installing. It sits at `10.10.0.2` on the
underlay and takes host octet `100` on every overlay network.

| Property                  | Value                                            |
| :------------------------ | :----------------------------------------------- |
| Primary overlay interface | `vx-<cluster6>-<vni4>` at `10.200.<hash>.100/24` |
| Secondary interfaces      | `sec-<cluster6>-<vlan_id>` at `<subnet>.100/24`  |
| MTU                       | 1410 on every overlay interface                  |

Cluster nodes use plain `vxlan0` and `gdcenet0.<vlan_id>` so that unmodified GDC
`Network` resources and MetalLB discover them. Shared hosts like this one need
cluster-scoped names, because they attach to every cluster at once.

The overlay configuration is mirrored to
`gs://gem-${PROJECT_ID}-overlay-sync/gem_admin_ws/` when the `vxlan` role runs,
and a root cron entry named `GEM VXLAN Overlay Synchronizer` pulls it back once
a minute. Only `/etc/systemd/network` files are covered. This is to ensure that
the overlay configuration is persisted outside of the admin workstation,
permitting the admin workstation to be rebuilt without losing connectivity to
GEM clusters.

## Security

The instance has no public address and is reachable only through IAP-brokered
SSH or from inside the VPC subnet. Both firewall rules are scoped to the
`http-server` and `https-server` network tags, which despite their names are
what permit SSH here, and the intra-subnet rule allows all TCP, UDP and ICMP
rather than just SSH.

It holds two credentials worth protecting:

- `/home/gem/bm-gcr.json` a downloaded service account key
- `/home/gem/.ssh/id_rsa` the admin workstation's SSH private key which is
  authorized on every cluster node and when Cloud Build is configured, stored in
  Secret Manager. Anyone with shell access as `gem` has cluster-admin on every
  GEM cluster in the project.

When Cloud Build is configured that same key is also added to the workstation's
own `authorized_keys`, so read access to the secret is read access to this host,
not just to the nodes.

The VM runs as the project's default Compute Engine service account with the
`cloud-platform` scope. Shell access therefore carries whatever project-wide API
access that account has been granted, which on most projects is substantial.

## Troubleshooting

### Newly built cluster nodes reject SSH

You see `Permission denied (publickey)` when Ansible or `bmctl` tries to reach
nodes as `gem`.

A `terraform apply` in `terraform/admin-workstation` removed the
`workstation_pubkey` instance metadata that the Ansible role published. The
resource does not declare `lifecycle { ignore_changes = [metadata] }`, so
Terraform prunes the out-of-band key, and nodes created afterwards get an empty
`ssh-keys` value.

Re-run the admin workstation playbook to republish the key, then recreate the
affected nodes:

```bash
cd ansible
CLUSTER_NAME=none ansible-playbook admin-workstation.yaml
```

### Cluster creation fails with a service account key error

The playbook fails with `Failed to create the Anthos Service Account Key`.
`bmctl` needs `/home/gem/bm-gcr.json` to pull images, and the playbook creates
it by minting a key for `baremetal-gcr@${PROJECT_ID}.iam.gserviceaccount.com`.

The usual cause is the organization policy
`constraints/iam.disableServiceAccountKeyCreation`, but the same message appears
for any failure of that step, including a missing `baremetal-gcr@` account or
missing permission to impersonate `tf-provisioner@`.

Either have the constraint lifted for the project, or create a key for
`baremetal-gcr@` by hand and copy it to `/home/gem/bm-gcr.json` on the admin
workstation with mode 0600, owned by `gem`.

### Cluster nodes are unreachable over the overlay

Check the synchronizer, then the interfaces:

```bash
sudo crontab -l | grep 'GEM VXLAN Overlay Synchronizer'
sudo PROJECT_ID=${PROJECT_ID} HOST_DIR=gem_admin_ws \
  /usr/local/sbin/gem-cron-overlay-sync.sh
gcloud storage ls gs://gem-${PROJECT_ID}-overlay-sync/gem_admin_ws/
networkctl status vx-<cluster6>-<vni4>
```

If a peer's underlay IP changed, the forwarding database entries baked into the
`.network` files are stale. The admin workstation's own IP is static, so this is
usually triggered by a rebuilt edge router or a recreated node:

```bash
cd ansible
CLUSTER_NAME=your-cluster-name ansible-playbook restore-vxlan.yaml
```

That re-runs the `vxlan` role across every host in the cluster, not just the
workstation.

### A `gem-network-operator` unit restarts forever after deleting a cluster

`ansible/cleanup.yaml` removes the unit along with the cluster, so this only
affects clusters torn down before it did. The unit has `Restart=always` and
points at a kubeconfig that no longer exists, so it loops forever. Remove any
leftovers by hand:

```bash
sudo systemctl disable --now gem-network-operator-${CLUSTER_NAME}
sudo rm /etc/systemd/system/gem-network-operator-${CLUSTER_NAME}.service
sudo systemctl daemon-reload
```

### Ansible reports no hosts matched

`ansible/inventory.sh` returns an empty inventory when the admin workstation's
Terraform state is missing, which makes every host disappear at once. It exits
non-zero without `CLUSTER_NAME`, without `PROJECT_ID` unless
`gcloud config get-value project` supplies one, and whenever the resolved zone
does not look like a zone. Apply the Terraform module first, then check what the
inventory sees:

```bash
cd ansible
PROJECT_ID=${PROJECT_ID} CLUSTER_NAME=none ./inventory.sh | jq '.workstation'
```

## Rebuilding the admin workstation

> [!WARNING]
> Only `/etc/systemd/network` overlay files are externalized to GCS. Everything
> under `/home/gem` lives on the boot disk: `bm-gcr.json`, every cluster's
> kubeconfig and bmctl workspace, and the SSH keypair. Of those, only the SSH
> private key has a copy elsewhere, in the `gem-cluster-builder-ssh-key` secret,
> and only if you configured Cloud Build. Deleting the instance orphans running
> clusters, which are then left with no admin credential except GKE Connect
> Gateway.

If you have to rebuild, re-run these in order:

```bash
cd ansible
CLUSTER_NAME=none ansible-playbook admin-workstation.yaml
CLUSTER_NAME=your-cluster-name ansible-playbook restore-vxlan.yaml
```

Run `restore-vxlan.yaml` once per cluster. The ordering matters: the workstation
playbook is what reinstalls the overlay synchronizer cron, and `restore-vxlan`
relies on it.

> [!CAUTION]
> The workstation playbook generates a **new** SSH keypair on the fresh disk and
> publishes the new public key to instance metadata. Nodes of clusters that
> already exist still authorize the old key, because their metadata was written
> when they were created. The rebuilt workstation therefore cannot SSH to them,
> which breaks `bmctl reset`, node pool operations and any further Ansible runs
> against those nodes. Restore the original private key from Secret Manager
> before running the playbook, or re-apply `terraform/cluster` for each affected
> cluster.

Cluster kubeconfigs are not recoverable this way. Use Connect Gateway for
clusters that already exist.

## Important files on disk

Worth knowing when you are debugging on the box:

| Path                                                         | Purpose                                          |
| :----------------------------------------------------------- | :----------------------------------------------- |
| `/home/gem/bmctl-workspace/<cluster>/<cluster>-kubeconfig`   | Cluster admin credential                         |
| `/home/gem/bmctl-workspace/<cluster>/<cluster>.yaml`         | Rendered cluster definition                      |
| `/home/gem/bmctl-workspace/<cluster>/log/create-cluster-*/`  | Build logs                                       |
| `/home/gem/bm-gcr.json`                                      | Anthos image-pull service account key, mode 0600 |
| `/home/gem/.ssh/id_rsa`                                      | Key authorized on all cluster nodes              |
| `/usr/local/sbin/bmctl-<version>`, `/usr/local/sbin/bmctl`   | Versioned binaries and active symlink            |
| `/usr/local/bin/gem-network-operator`                        | The operator binary, shared by every cluster     |
| `/usr/local/sbin/gem-cron-overlay-sync.sh`                   | Overlay synchronizer                             |
| `/etc/systemd/network/10-{vx,sec}-*.{netdev,network}`        | Overlay interface definitions                    |
| `/etc/systemd/system/gem-network-operator-<cluster>.service` | Per-cluster operator                             |
| `/etc/systemd/system/vxlan-tcpmss-<cluster>.service`         | Per-cluster MSS clamping                         |
| `/etc/docker/daemon.json`                                    | Sets Docker's MTU to 1410 for KinD               |

The operator binary is shared, so rebuilding one cluster's operator replaces the
binary every cluster's unit runs.

## Related documentation

- [Project Setup](project-setup.md) for the prerequisites and service accounts
- [GEM Networking](gem-networking.md) for the overlay addressing scheme
- [Cloud Build](cloud-build.md) for the SSH key handshake this host performs
- [Edge Router](edge-router.md) for service tunnels
