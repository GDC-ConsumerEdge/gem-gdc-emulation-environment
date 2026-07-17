# GEM Edge Router

The GEM Edge Router (`gem-edge-router`) is a specialized ingress proxy virtual machine that facilitates secure, stable external connectivity from a developer's local workstation directly into the emulated L2 secondary networks (Multus VLANs) and MetalLB VIPs of the emulated GDC Connected environment.

## Purpose

Physical GDC Connected servers are integrated directly into a customer's on-premises network infrastructure. In that environment, MetalLB VIPs and Kubernetes services are directly routeable from the developer's workstation (e.g., navigating to `http://192.168.200.x`).

In a virtualized GCE emulation environment, these VIPs exist inside emulated L2 VXLAN tunnels and are invisible to GCP's VPC routing. The GEM Edge Router sits on both networks, acting as a bridging gateway. It terminates external ingress tunnels (via GCP Identity-Aware Proxy) and forwards TCP traffic directly into the internal MetalLB LoadBalancer VIPs and KubeVirt VM IPs.



## Design Decisions

### 1. Traefik Edge Ingress Proxy
Instead of using complex, resource-heavy ingress controllers inside the cluster, the Edge Router runs a standalone instance of **Traefik**. Traefik serves as a dynamic, lightweight edge router. It automatically routes inbound TCP/UDP traffic directly to MetalLB LoadBalancer IPs on secondary networks, completely bypassing the Kubernetes control plane.

### 2. Multi-homed Network
The Edge Router VM is configured with a multi-homed network:
*   **Primary Underlay Interface (`ens4`)**: Participates in the GCP VPC (`10.10.0.0/24`), which allows external users to connect securely via IAP.
*   **Virtual Overlay Interfaces (`vx-*`, `sec-*`)**: Emulated L2 networks using VXLAN on top of the VPC. The Edge Router participates in all emulated VLAN overlays, giving it direct L2 access to the GDC nodes.

### 3. Ephemeral and State-Free Design (GCS Sync)
To ensure the Edge Router VM can be completely deleted, rebuilt, or scaled down on-demand without losing networking tunnel maps:
*   All virtual device (`.netdev`) and systemd-network (`.network`) configs are stored in a centralized, host-segregated GCS bucket path (`gs://gem-${PROJECT_ID}-overlay-sync/edge_router_host/`).
*   A background cron job runs every minute on the Edge Router, syncing these configs locally using deletion-aware `gcloud storage rsync`.
*   When a new GEM cluster is spun up or deleted, its VXLAN configs are uploaded to the Edge Router GCS folder, and the Edge Router's background daemon dynamically adds or prunes the overlay interfaces.

### 4. TCP MSS Clamping & MTU
Because GCP's physical VPC network enforces a 1460 MTU limit and VXLAN encapsulation adds a 50-byte header overhead, all overlay interfaces on the Edge Router are strictly configured with an MTU of 1410. To prevent packet dropping and TLS handshake timeouts (due to DF flags on large payloads), an iptables TCP MSS clamping ruleset is automatically applied to the VXLAN interface.



## Installation and Configuration

### 1. Provisioning (Terraform)
Run from the root repository directory on your local machine to provision the GCE instance:
```bash
cd terraform/edge-router

terraform init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=edge-router/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform apply
```

### 2. Configuration (Ansible)
Run the playbook to bootstrap the OS, install Traefik, enable forwarding, and configure the GCS synchronizer:
```bash
cd ansible

CLUSTER_NAME=none ansible-playbook -i inventory.sh edge-router.yaml
```
*Note: Setting `CLUSTER_NAME=none` ensures the playbook only provisions base shared router infrastructure and does not attempt to connect to active workloads.*

During configuration, Ansible will automatically:
*   Perform an OS `apt update` and `dist-upgrade` (triggering a reboot if a new Linux kernel is installed).
*   Enable IP forwarding globally (`net.ipv4.ip_forward = 1`) so the kernel can bridge traffic between VPC and VXLAN.
*   Install and configure Traefik as a systemd service.
*   Deploy the GCS VXLAN overlay synchronizer cron job.



## 🛠️ Troubleshooting

### Tunnel ERROR: `connect failed: No route to host`
If attempting to connect via `gem-tunnel.sh` returns a `connect failed: No route to host` error, the Edge Router is unable to bridge traffic into the VXLAN.

#### Check the Underlay IP
If you recently deleted and rebuilt the Edge Router, GCP likely assigned it a new dynamic underlay IP (e.g., changing from `10.10.0.4` to `10.10.0.8`).
1.  Check the Edge Router's actual underlay IP:
    ```bash
    gcloud compute instances describe gem-edge-router --format="value(networkInterfaces[0].networkIP)"
    ```
2.  Check if the dynamic inventory matches the live VM:
    ```bash
    CLUSTER_NAME=gemcluinstances ./inventory.sh | grep -A 4 edge_router_host
    ```

#### Check systemd-networkd Stale Links
If the underlay IP changed, `systemd-networkd` on the Edge Router will refuse to modify existing kernel interfaces, resulting in a stale device binding in the kernel.
1.  SSH into the Edge Router and check the detailed link binding:
    ```bash
    ip -d link show dev vx-gemclu-9355
    ```
    If the `local` IP listed there does not match the Edge Router's current underlay IP, the kernel is dropping all overlay encapsulation.
2.  Resolution: Regenerate the mesh configs and force-rebuild the virtual interfaces:
    ```bash
    # Run from your local machine to update GCS and the node FDB tables
    CLUSTER_NAME=your-cluster-name ansible-playbook -i inventory.sh restore-vxlan.yaml

    # Run on the Edge Router VM to purge the old links and force recreation
    sudo ip link delete vx-gemclu-9355
    sudo ip link delete sec-gemclu-123
    sudo ip link delete sec-gemclu-456
    sudo systemctl restart systemd-networkd
    ```

#### Flush Node ARP Caches
When the Edge Router is rebuilt, it gets a brand-new virtual MAC address. However, the GDC cluster nodes (`node1`, `node2`, `node3`) will continue sending response packets to the old MAC address cached in their ARP tables.
1.  Verify if the node ARP table has a `STALE` entry for the Edge Router (`10.200.X.254`):
    ```bash
    # Run on one of the cluster nodes (e.g., node2)
    ip neigh show dev vxlan0
    ```
2.  **Resolution**: Flush the ARP tables on all cluster nodes:
    ```bash
    # Run on each cluster node
    sudo ip neigh flush dev vxlan0
    ```



### Traefik Service Status
If the tunnel establishes but HTTP/VNC connections time out:
1.  Check if Traefik is running:
    ```bash
    sudo systemctl status traefik
    ```
2.  Inspect Traefik's logs:
    ```bash
    sudo journalctl -u traefik -n 100
    ```
