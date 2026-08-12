# GEM Secondary Networking (`gem-network-operator`) Implementation Guide

## Objective
This document provides a highly detailed, decision-free specification for implementing Secondary Networking (Island Mode) in the GEM environment. The goal is to build a "clean room" implementation of the GDC Connected `nf-operator` inside GEM. This allows users to apply unmodified GDC Gateway API and Network CRDs to GEM, while authentic data-plane routing is emulated correctly under the hood via standard Kubernetes primitives and static Ansible host-networking configurations.

## Architecture & Constraints
1. **Static Host Interfaces**: Linux VXLAN bridges (`gdcenet0.<vlan_id>`) will be statically rendered via Ansible at cluster build-time. We will **not** build a Node Agent DaemonSet.
2. **Accept Unmodified Manifests**: We must accept dynamically applied `Network` and Gateway API CRs gracefully without webhook rejection. If the host network is missing, we communicate failure via Kubernetes `Status` conditions and `Events`.
3. **No Secondary Egress NAT**: Emulating "Island Mode" means pods route out their default interface (`eth0`) for general internet egress. The Edge Router needs no outbound masquerade `iptables` rules for the secondary VLANs.
4. **Strict MTU 1410**: All VXLAN overlay interfaces on nodes and the Edge Router must strictly use MTU **1410** (1460 GCE VPC MTU - 50-byte VXLAN encapsulation header).

---

## Phase 1: Ansible & Base Infrastructure Updates

### 1.1 IPAM Default Fix
Inside `ansible/group_vars/all.yaml` (or where the default secondary networks are defined), modify the default `per_node_ipam_size` from `18` to `24`. A `/18` drastically limits the cluster to just 4 nodes for that subnet. A `/24` allows 256 IPs per node.

### 1.2 Edge Router Top-of-Rack Emulation
Presently, no machine binds the specified `gateway` IP. To emulate the Top of Rack switch:
- In `ansible/roles/vxlan`, modify the logic for the Edge Router.
- The Edge Router's `sec-<truncated_cluster>-<vlan_id>` overlay interface must explicitly bind the `gateway` IP address defined in the `secondary_networks` variables (e.g. `21.0.119.254`).
- Do **not** apply `iptables -t nat -A POSTROUTING` rules for this network, as egress is handled by Default networking.

### 1.3 Remove Static ABM Manifest Templating
Currently, `ansible/roles/secondary_networks` statically loops and generates `ClusterCIDRConfig`, `NetworkAttachmentDefinition`, and `IPAddressPool` CRs.
- **Action:** Delete or comment out the `.j2` templates for these resources in Ansible.
- Ansible must *only* deploy the `Network` CRD. The new `gem-network-operator` (Phase 2) will take over dynamic generation of these resources based on the presence of the `Network` CR.

---

## Phase 2: Create the `gem-network-operator` Workstation Daemon
To eliminate any requirement for the end user to build, tag, and push container images to a remote container registry, `gem-network-operator` runs natively as a **`systemd` host daemon on the Admin Workstation (`gem-admin-ws`)**.

The operator connects to the cluster using the generated `cluster-kubeconfig`, managing all GDC custom resources, child objects, dynamic VIP allocations, and CoreDNS mappings out-of-band.

### 2.1 Deployment & Service Lifecycle
* **Binary Location:** `/usr/local/bin/gem-network-operator` on the Admin Workstation.
* **Systemd Unit:** `/etc/systemd/system/gem-network-operator-{{ cluster_name }}.service` (templated and started automatically by Ansible during cluster creation).
* **Flags Supported:** Accepts `--kubeconfig`, `--metrics-bind-address`, and `--health-probe-bind-address`.

### 2.2 CRD Definitions
The operator hosts and applies the definitions for these CRDs into the cluster so user manifests are accepted:
- `networking.gke.io/v1 Network`
- `networking.gke.io/v1 GKEGatewayCIDR`
- `networking.gke.io/v1 GKEL4Route`
- `networking.gke.io/v1 GKEEndpointSelector`
- `gateway.networking.k8s.io/v1 Gateway`
- `gateway.networking.k8s.io/v1 GatewayClass` (Specifically `controllerName: "networking.gke.io/cluster-ip"`)

### 2.2 Reconciler 1: `Network` Translation & Status Contract
Watch the `networking.gke.io/v1 Network` CR. Upon reconciliation:

#### Guardrail Validation (Static Host Limitation)
- Check the `Network`'s `vlan-id` (annotation `networking.gke.io/gdce-vlan-id`) or interface matcher against a pre-loaded configuration map injected by Ansible during build.
- If the network was *not* pre-provisioned via Ansible:
  - Do NOT reject or crash context.
  - Set `Network.Status.Conditions`: `Type: Ready`, `Status: "False"`, `Reason: MissingHostInterface`.
  - Provide a clear message: `"Network accepted, but the underlying host VXLAN was not statically provisioned. Pods attempting to use this network will fail. Rebuild cluster using Ansible variables to activate."`
  - Emit a Kubernetes `Warning` Event on the object.

#### Child Resource Translation
If valid, automatically generate/update the following K8s objects owned by the `Network` CR:
- `NetworkAttachmentDefinition` for Multus, mapped to `spec.nodeInterfaceMatcher.interfaceName`.
- `ClusterCIDRConfig` taking its `perNodeMaskSize` from `annotations.networking.gke.io/gdce-per-node-ipam-size` and its subset from `annotations.networking.gke.io/gke-gateway-pod-cidr`.
- `IPAddressPool` and `L2Advertisement` (MetalLB) sourced from the `annotations.networking.gke.io/gdce-lb-service-vip-cidrs` JSON array content.

#### Status Conditions Contract
The operator must populate the exact status conditions expected by GDC consumers and E2E tests:
- `Type: Ready`, `Status: "True"`, `Reason: NetworkReady`
- `Type: CoreDNSReady`, `Status: "True"`, `Reason: CoreDNSServiceReady`, `Message: CoreDNS service is ready for the network`

---

### 2.3 Reconciler 2: Multi-Network Services `Gateway` Translation
Because standard K8s Services use `eth0` (primary pod IPs) instead of the Multus `net1` IPs, we must use a workaround via `externalIPs`.

Watch `Gateway`, `GKEGatewayCIDR`, `GKEL4Route`, and `GKEEndpointSelector`. Upon reconciliation of a linked Gateway structure:

1. **Allocate IP:** Select an IP address from the matching `GKEGatewayCIDR` pool.
2. **Update Gateway Status:**
   - Set `status.addresses: [{type: IPAddress, value: "<allocated_ip>"}]`.
   - Set `status.conditions`:
     - `Type: Accepted`, `Status: "True"`, `Reason: Accepted`
     - `Type: Programmed`, `Status: "True"`, `Reason: Programmed`, `Message: Gateway programmed and IP allocation prepared`
3. **Update GKEL4Route Status:**
   - Set `status.conditions`:
     - `Type: Accepted`, `Status: "True"`, `Reason: Accepted`
     - `Type: Ready`, `Status: "True"`, `Reason: Ready`
4. **Dynamically Create Service:** Create a native Kubernetes `Service` object.
   - Set `spec.clusterIP` to `None`.
   - Inject the allocated IP into **`spec.externalIPs`**. (This forces kube-proxy to load-balance traffic hitting this IP).
5. **Endpoint Scraping:** Query the K8s API for Pods matching the labels in `GKEEndpointSelector.spec.selector`.
   - Inspect both `networking.gke.io/pod-ips` and `k8s.v1.cni.cncf.io/network-status` annotations.
   - Extract the IP address explicitly assigned to the secondary network (`eth1` / target VLAN).
6. **Dynamically Create EndpointSlice:** Create a native `EndpointSlice` object linked to the dynamic Service.
   - Populate `endpoints[].addresses[]` with the **secondary network IPs** scraped in step 5.
   - *Result: kube-proxy will DNAT externalIP traffic correctly out the VXLAN to the pod's `eth1`.*
7. **DNS Masking (`.gkegw.cluster.local`):** Configure CoreDNS so `gateway-name.gateway-namespace.gkegw.cluster.local` resolves directly to the dynamic `externalIP`.

---

### 2.4 Mutating Admission Webhook: Pod Route Injection
In GDC, pods are immutable after creation. When pods attach to a secondary network via `networking.gke.io/interfaces`:
- The mutating webhook must inspect active `GKEGatewayCIDR` definitions for the attached network.
- Inject kernel routing rules (`<gateway_cidr> via <gateway4> dev eth1`) and populate `networking.gke.io/interface-status`.
- *Why:* If the route to the `GKEGatewayCIDR` is not injected into the pod at scheduling time, outgoing traffic to Gateway VIPs will time out.

---

### 2.5 Finalizer & Teardown Lifecycle Management
To avoid namespace deletion deadlocks, the operator must implement standard GDC finalizers:
- **`GKEL4Route` Finalizer (`networking.gke.io/gkel4route-enpointslice-finalizer`):** Reconciler cleans up the dynamic `EndpointSlice` and removes the finalizer when the route is marked for deletion.
- **`Gateway` Finalizer (`networking.gke.io/gateway-ip-protection`):** Reconciler releases the allocated IP back to the `GKEGatewayCIDR` pool and cleans up the dynamic `Service` before removing the finalizer.
- **`Network` Finalizer (`networking.gke.io/network-finalizer`):** Reconciler cleans up child `NetworkAttachmentDefinition`, `ClusterCIDRConfig`, and MetalLB pools before releasing the network.

---

## Phase 3: Verification & E2E Validation

Validation must be executed against the Chainsaw E2E suites located in `tests/e2e/secondary-networks/`:

1. **Intra-Node Suite** (`tests/e2e/secondary-networks/intra-node/`):
   * Validates local pod-to-pod reachability, Gateway API DNS resolution (`*.gkegw.cluster.local`), and LoadBalancer VIP allocation using `podAffinity`.
   * Run: `chainsaw test tests/e2e/secondary-networks/intra-node`
2. **Cross-Node Suite** (`tests/e2e/secondary-networks/cross-node/`):
   * Validates multi-node VXLAN encapsulation, cross-host Gateway routing, and multi-node load balancing using `podAntiAffinity`.
   * Run: `chainsaw test tests/e2e/secondary-networks/cross-node`
