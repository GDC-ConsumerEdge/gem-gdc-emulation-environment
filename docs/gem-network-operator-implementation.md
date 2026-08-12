# GEM Secondary Networking (`gem-network-operator`) Implementation Guide

> **Status: Implemented.** This document was originally written as a build spec before the work started; it has since been updated to describe what was actually built, with deviations from the original plan called out inline (search for "**Current implementation note**" and §2.7 "Known Gaps"). Treat it as the current source of truth for `gem-network-operator` behavior, not as a TODO list.

## Objective
This document provides a highly detailed, decision-free specification for implementing Secondary Networking (Island Mode) in the GEM environment. The goal is to build a "clean room" implementation of the GDC Connected `nf-operator` inside GEM. This allows users to apply unmodified GDC Gateway API and Network CRDs to GEM, while authentic data-plane routing is emulated correctly under the hood via standard Kubernetes primitives and static Ansible host-networking configurations.

## Architecture & Constraints
1. **Static Host Interfaces**: Linux VXLAN bridges (`gdcenet0.<vlan_id>`) will be statically rendered via Ansible at cluster build-time. We will **not** build a Node Agent DaemonSet.
2. **Accept Unmodified Manifests**: We must accept dynamically applied `Network` and Gateway API CRs gracefully without webhook rejection. If the host network is missing, we communicate failure via Kubernetes `Status` conditions and `Events`.
3. **No Secondary Egress NAT**: Emulating "Island Mode" means pods route out their default interface (`eth0`) for general internet egress. The Edge Router needs no outbound masquerade `iptables` rules for the secondary VLANs.
4. **Strict MTU 1410**: All VXLAN overlay interfaces on nodes and the Edge Router must strictly use MTU **1410** (1460 GCE VPC MTU - 50-byte VXLAN encapsulation header).

---

## Phase 1: Ansible & Base Infrastructure Updates

### 1.1 IPAM Default Fix — Done
`ansible/group_vars/all.yaml`'s `secondary_networks` entries use `per_node_ipam_size: 24`. A `/18` would have drastically limited the cluster to just 4 nodes for that subnet; `/24` allows 256 IPs per node.

### 1.2 Edge Router Top-of-Rack Emulation — Done
The Edge Router's `sec-<truncated_cluster>-<vlan_id>` overlay interface binds the network's `gateway` IP address directly (`ansible/roles/vxlan/tasks/main.yaml`, conditioned on `inventory_hostname == 'edge_router_host'`). No `iptables -t nat -A POSTROUTING` rules are applied for these networks — egress is handled by the pod's default (primary) network.

### 1.3 Remove Static ABM Manifest Templating — Done
Ansible's `secondary_networks` role no longer renders or applies `ClusterCIDRConfig`, `NetworkAttachmentDefinition`, or `IPAddressPool` CRs at cluster-build time — it only applies the `Network` CRD instances, and `gem-network-operator` dynamically generates the child resources at runtime as designed. The old `.j2` templates for those resources (`clustercidrconfig.yaml.j2`, `metallb.yaml.j2`) have been removed.

---

## Phase 2: Create the `gem-network-operator` Workstation Daemon
To eliminate any requirement for the end user to build, tag, and push container images to a remote container registry, `gem-network-operator` runs natively as a **`systemd` host daemon on the Admin Workstation (`gem-admin-ws`)**.

The operator connects to the cluster using the generated `cluster-kubeconfig`, managing all GDC custom resources, child objects, dynamic VIP allocations, and CoreDNS mappings out-of-band.

### 2.1 Deployment & Service Lifecycle
* **Binary Location:** `/usr/local/bin/gem-network-operator` on the Admin Workstation, built from source by Ansible (`go build`) rather than pulled as a container image — there is no registry dependency.
* **Systemd Unit:** `/etc/systemd/system/gem-network-operator-{{ cluster_name }}.service` (templated and started automatically by Ansible during cluster creation).
* **Kubeconfig:** There is no `--kubeconfig` flag. The manager resolves credentials via `ctrl.GetConfig()`, which the systemd unit drives by setting the `KUBECONFIG` environment variable to the cluster's generated kubeconfig.
* **Flags Supported:** `--metrics-bind-address` and `--health-probe-bind-address` (both controller-runtime standard flags). The shipped systemd unit passes `--metrics-bind-address=0 --health-probe-bind-address=0` to disable both endpoints — multiple clusters' operator instances run on the same shared Admin Workstation, and enabling these on the default ports causes a bind collision between them. Do not re-enable without allocating a unique port per cluster instance.

### 2.2 CRD Definitions
The following CRDs are required for user manifests to be accepted. They are **not** applied by the operator itself — Ansible copies the static YAML from `operators/gem-network-operator/crds/` to the Admin Workstation and `kubectl apply`s it during cluster build (`ansible/roles/secondary_networks/tasks/main.yaml`), before the operator binary is ever started:
- `networking.gke.io/v1 Network`
- `networking.gke.io/v1 GKEGatewayCIDR`
- `networking.gke.io/v1 GKEL4Route`
- `networking.gke.io/v1 GKEEndpointSelector`
- `gateway.networking.k8s.io/v1 Gateway` / `GatewayClass` (from the upstream Gateway API "standard" CRD bundle, `gateway.networking.k8s.io_standard_install.yaml`)

Note: no `GatewayClass` object (e.g. `gke-cluster-ip` with `controllerName: "networking.gke.io/cluster-ip"`) is created by either Ansible or the operator today. `Gateway.spec.gatewayClassName` is accepted as an opaque string — the operator does not validate it against an actual `GatewayClass`, since GEM has no real multi-controller `GatewayClass` selection to emulate.

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

1. **Allocate IP:** Select an IP address from the matching `GKEGatewayCIDR` pool. **Current implementation note:** this is not a tracked allocation — the reconciler always returns the CIDR's first host address (network address + 1). There is no bookkeeping of already-issued addresses and no exhaustion handling, so two `Gateway`s referencing the same `GKEGatewayCIDR` will be assigned the same VIP and collide. This is sufficient for today's one-Gateway-per-network test topology but must be fixed (real IPAM/allocation tracking) before supporting more than one Gateway per secondary network. A `Gateway.spec.addresses[0].value` set explicitly takes precedence over this derivation and skips allocation entirely.
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

### 2.4 Pod Route Injection: No Admission Webhook Required
The original design called for a mutating admission webhook to inject secondary-network routes into pods at scheduling time (mirroring GDC's `nf-operator`). **This was not implemented, and is not needed.** Instead:
- `reconcileNetAttachDef` (in `network_controller.go`) renders each `Network`'s `NetworkAttachmentDefinition` using the **`macvlan`** CNI plugin in bridge mode with **`host-local`** IPAM, setting IPAM's `gateway` key to the `Network`'s `gateway4`.
- The `host-local` CNI IPAM plugin installs the default route for that gateway into the pod's network namespace itself, at CNI `ADD` time — this is standard CNI behavior, not custom GEM logic.
- Net effect: a pod that lists a `Network` in `networking.gke.io/interfaces` gets a working route to that network's Gateway VIP automatically, with no webhook, no `networking.gke.io/interface-status` population, and no dependency on pod scheduling order relative to the webhook.
- *Caveat:* because there's no webhook to reject or repair a misconfigured pod, a pod created before its `NetworkAttachmentDefinition` exists will simply fail to attach the interface (Multus will error at pod start) rather than receive a clear `interface-status` condition. Constraint 1 in [docs/secondary-networks.md](secondary-networks.md) (`GKEGatewayCIDR` before pods) still applies for the same underlying reason — the CIDR must exist so its network is fully described before workloads depend on it.

---

### 2.5 Finalizer & Teardown Lifecycle Management
**Current implementation note:** only two finalizers are actually implemented; there is no separate `GKEL4Route` finalizer.
- **`Gateway` Finalizer (`networking.gke.io/gateway-ip-protection`):** Reconciler deletes the dynamic `Service` and `EndpointSlice` before removing the finalizer. (There is no allocation pool to release back to, per the note in §2.3 — VIP "allocation" isn't tracked.)
- **`Network` Finalizer (`networking.gke.io/network-finalizer`):** Currently only removes the finalizer on deletion; it does **not** explicitly clean up the child `NetworkAttachmentDefinition`s, `IPAddressPool`/`L2Advertisement`, or (non-functional) `ClusterCIDRConfig` it created. Those objects are left behind and rely on namespace/cluster teardown (`ansible/cleanup.yaml`) to remove them along with everything else.
- `GKEL4Route` and `GKEEndpointSelector` have no finalizers — they're treated as pure inputs read by the `Gateway` reconciler, not owned resources with their own cleanup lifecycle.

### 2.6 Service → MetalLB Binding (undocumented alternate path)
Separately from the Gateway API flow above, `NetworkReconciler.reconcileServices` watches all `Service`s. Any `Service` annotated `networking.gke.io/network: <network-name>` gets `metallb.universe.tf/address-pool: <network-name>-pool` applied automatically, binding it to that network's MetalLB `IPAddressPool`. This is unit-tested (`network_controller_test.go`) but not exercised by any e2e suite and not part of the GDC-compatible CRD surface — it's a GEM-only convenience for binding a plain `Service` (as opposed to a Gateway-fronted one) to a secondary network's VIP pool.

### 2.7 Known Gaps / Non-Functional Paths
- **`ClusterCIDRConfig` reconciliation is dead code.** `reconcileClusterCIDRConfig` creates `networking.gke.io/v1alpha1 ClusterCIDRConfig` objects when a `Network`'s `networking.gke.io/gke-gateway-pod-cidr` annotation is set, but no CRD of that kind is installed anywhere in GEM (see §2.2's CRD list) — the create call fails and is logged, not fatal, so reconciliation otherwise proceeds normally. Either install a matching CRD and wire this up for real, or remove the reconciliation path.

---

## Phase 3: Verification & E2E Validation

Validation must be executed against the Chainsaw E2E suites located in `tests/e2e/secondary-networks/`:

1. **Intra-Node Suite** (`tests/e2e/secondary-networks/intra-node/`):
   * Validates local pod-to-pod reachability, Gateway API DNS resolution (`*.gkegw.cluster.local`), and LoadBalancer VIP allocation using `podAffinity`.
   * Run: `chainsaw test tests/e2e/secondary-networks/intra-node`
2. **Cross-Node Suite** (`tests/e2e/secondary-networks/cross-node/`):
   * Validates multi-node VXLAN encapsulation, cross-host Gateway routing, and multi-node load balancing using `podAntiAffinity`.
   * Run: `chainsaw test tests/e2e/secondary-networks/cross-node`
