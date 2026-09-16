# GEM Secondary Networking (`gem-network-operator`) Implementation Guide

> **Status: Implemented.** This document was originally written as a build spec
> before the work started; it has since been updated to describe what was
> actually built, with deviations from the original plan called out inline
> (search for "**Current implementation note**" and §2.8 "Known Gaps"). Treat it
> as the current source of truth for `gem-network-operator` behavior, not as a
> TODO list.

## Objective

This document provides a highly detailed, decision-free specification for
implementing Secondary Networking (Island Mode) in the GEM environment. The goal
is to build a "clean room" implementation of the GDC Connected `nf-operator`
inside GEM. This allows users to apply unmodified GDC Gateway API and Network
CRDs to GEM, while authentic data-plane routing is emulated correctly under the
hood via standard Kubernetes primitives and static Ansible host-networking
configurations.

## Architecture and Constraints

1. **Static Host Interfaces**: Linux VXLAN bridges (`gdcenet0.<vlan_id>`) will
   be statically rendered via Ansible at cluster build-time. We will **not**
   build a Node Agent DaemonSet.
2. **Accept Unmodified Manifests**: We must accept dynamically applied `Network`
   and Gateway API CRs gracefully without webhook rejection. If the host network
   is missing, we communicate failure via Kubernetes `Status` conditions and
   `Events`.
3. **No Secondary Egress NAT**: Emulating "Island Mode" means pods route out
   their default interface (`eth0`) for general internet egress. The Edge Router
   needs no outbound masquerade `iptables` rules for the secondary VLANs.
4. **Strict MTU 1410**: All VXLAN overlay interfaces on nodes and the Edge
   Router must strictly use MTU **1410** (1460 GCE VPC MTU - 50-byte VXLAN
   encapsulation header).

______________________________________________________________________

## Phase 1: Ansible and Base Infrastructure Updates

### 1.1 IPAM Default Fix (Done)

`ansible/group_vars/all.yaml`'s `secondary_networks` entries use
`per_node_ipam_size: 24`. A `/18` would have drastically limited the cluster to
just 4 nodes for that subnet; `/24` allows 256 IPs per node.

### 1.2 Edge Router Top-of-Rack Emulation (Done)

The Edge Router's `sec-<truncated_cluster>-<vlan_id>` overlay interface binds
the network's `gateway` IP address directly
(`ansible/roles/vxlan/tasks/main.yaml`, conditioned on
`inventory_hostname == 'edge_router_host'`). No `iptables -t nat -A POSTROUTING`
rules are applied for these networks. Egress is handled by the pod's default
(primary) network.

### 1.3 Remove Static ABM Manifest Templating (Done)

Ansible's `secondary_networks` role no longer renders or applies
`ClusterCIDRConfig`, `NetworkAttachmentDefinition`, or `IPAddressPool` CRs at
cluster-build time. It only applies the `Network` CRD instances, and
`gem-network-operator` dynamically generates the child resources at runtime as
designed. The old `.j2` templates for those resources
(`clustercidrconfig.yaml.j2`, `metallb.yaml.j2`) have been removed.

______________________________________________________________________

## Phase 2: Create the `gem-network-operator` Workstation Daemon

To eliminate any requirement for the end user to build, tag, and push container
images to a remote container registry, `gem-network-operator` runs natively as a
**`systemd` host daemon on the Admin Workstation (`gem-admin-ws`)**.

The operator connects to the cluster using the generated `cluster-kubeconfig`,
managing all GDC custom resources, child objects, dynamic VIP allocations, and
CoreDNS mappings out-of-band.

### 2.1 Deployment and Service Lifecycle

- **Binary Location:** `/usr/local/bin/gem-network-operator` on the Admin
  Workstation, built from source by Ansible (`go build`) rather than pulled as a
  container image, so there is no registry dependency.

- **Systemd Unit:**
  `/etc/systemd/system/gem-network-operator-{{ cluster_name }}.service`
  (templated and started automatically by Ansible during cluster creation).

- **Kubeconfig:** The manager resolves credentials via `ctrl.GetConfig()`, which
  accepts controller-runtime's built-in `--kubeconfig` flag, the `KUBECONFIG`
  environment variable, or in-cluster configuration. The shipped systemd unit
  uses `KUBECONFIG`, pointed at the cluster's generated kubeconfig.

- **Flags Supported:**

  | Flag                          | Default     | Notes                                                                   |
  | :---------------------------- | :---------- | :---------------------------------------------------------------------- |
  | `--metrics-bind-address`      | `:8080`     | Set to `0` by the systemd unit                                          |
  | `--health-probe-bind-address` | `:8081`     | Set to `0` by the systemd unit                                          |
  | `--webhook-port`              | `0`         | `0` disables the webhook entirely. The unit derives a per-cluster value |
  | `--webhook-host`              | `10.10.0.2` | Advertised in the webhook's client URL and its certificate SANs         |
  | `--webhook-cert-dir`          | `""`        | Defaults to `/tmp/gem-network-operator-certs-<port>`                    |
  | `--leader-elect`              | `false`     | Leader election ID `gem-network-operator.gke.io`                        |
  | `--zap-*`                     |             | Standard controller-runtime logging flags                               |

  The shipped unit passes
  `--metrics-bind-address=0 --health-probe-bind-address=0` to disable both
  endpoints, because multiple clusters' operator instances run on the same
  shared Admin Workstation and the default ports would collide. Do not re-enable
  without allocating a unique port per cluster instance.

  The unit also passes `--webhook-port` and `--webhook-host`. Without a non-zero
  `--webhook-port`, the pod mutating webhook described in §2.5 does not run at
  all.

> [!WARNING]
> The webhook port is derived as `9443 + md5(cluster_name) % 500`. That is a
> hash, not an allocation, so two clusters on the same workstation can collide
> and the second operator fails to bind. Set `webhook_port` explicitly for one
> of them if that happens.
>
> The unit template also resolves `--webhook-host` through
> `hostvars['admin_workstation_host']`, but the inventory names that host
> `gem_admin_ws`. The lookup therefore always falls through to the literal
> `10.10.0.2`. That matches the Terraform default for `workstation_ip`, so it
> works today, but changing `workstation_ip` silently breaks the webhook.

### 2.2 CRD Definitions

The following CRDs are required for user manifests to be accepted. They are
**not** applied by the operator itself. Ansible copies the static YAML from
`operators/gem-network-operator/crds/` to the Admin Workstation and
`kubectl apply`s it during cluster build
(`ansible/roles/secondary_networks/tasks/main.yaml`), before the operator binary
is ever started:

- `networking.gke.io/v1 Network`
- `networking.gke.io/v1 GKEGatewayCIDR`
- `networking.gke.io/v1 GKEL4Route`
- `networking.gke.io/v1 GKEEndpointSelector`
- `gateway.networking.k8s.io/v1 Gateway` / `GatewayClass` (from the upstream
  Gateway API "standard" CRD bundle,
  `gateway.networking.k8s.io_standard_install.yaml`)

Note: no `GatewayClass` object (e.g. `gke-cluster-ip` with
`controllerName: "networking.gke.io/cluster-ip"`) is created by either Ansible
or the operator today. `Gateway.spec.gatewayClassName` is accepted as an opaque
string. The operator does not validate it against an actual `GatewayClass`,
since GEM has no real multi-controller `GatewayClass` selection to emulate.

### 2.3 Reconciler 1: `Network` Translation and Status Contract

Watch the `networking.gke.io/v1 Network` CR. Upon reconciliation:

#### Guardrail Validation (Static Host Limitation)

Secondary networks only function when Ansible statically provisioned the
matching host VXLAN interface at cluster build time; there is no supported path
to add one to a live cluster. The reconciler enforces visibility of violations:

- Ansible publishes a **`gem-provisioned-networks` ConfigMap in `kube-system`**
  at cluster build (rendered from the `secondary_networks` group_vars list by
  `ansible/roles/secondary_networks`; key = `vlan_id`, value = interface name).
- The reconciler checks the `Network`'s `vlan-id` annotation
  (`networking.gke.io/gdce-vlan-id`) or resolved interface name against it.
- If the network was *not* pre-provisioned (or the ConfigMap is absent):
  - It does NOT reject or crash. Child resources are still created, per the GDC
    contract.
  - It sets `Network.Status.Conditions`: `Type: Ready`, `Status: "False"`,
    `Reason: MissingHostInterface` with message
    `"Network accepted, but the underlying host VXLAN was not statically provisioned. Pods attempting to use this network will fail. Rebuild cluster using Ansible variables to activate."`
  - It emits a Kubernetes `Warning` Event on the object (recorder registered as
    `gem-network-operator`).

#### Child Resource Translation

If valid, the reconciler generates/updates the following objects, each carrying
an `OwnerReference` back to the `Network` CR:

- `NetworkAttachmentDefinition` for Multus, mapped to
  `spec.nodeInterfaceMatcher.interfaceName`, in every active namespace.
- `IPAddressPool` and `L2Advertisement` (MetalLB) sourced from the
  `annotations.networking.gke.io/gdce-lb-service-vip-cidrs` JSON array content.

Writes are diffed first (no-op reconciles don't rewrite unchanged objects) and
retried on optimistic-concurrency conflicts. The
`networking.gke.io/gke-gateway-pod-cidr` and
`networking.gke.io/gdce-per-node-ipam-size` annotations are still accepted on
`Network` CRs (and rendered by `network.yaml.j2`) for GDC-manifest parity, but
are **ignored**. The `ClusterCIDRConfig` path they used to feed was removed (see
§2.8).

#### Status Conditions Contract

Conditions reflect actual reconcile outcomes:

- `Type: Ready`: `"True"/NetworkReady` when the host interface is provisioned
  and all child resources reconciled; `"False"/MissingHostInterface` per the
  guardrail above; `"False"/ChildResourceError` (message names the failing
  child) when any child-resource write failed.
- `Type: CoreDNSReady`: `"True"/CoreDNSServiceReady` when the
  `.gkegw.cluster.local` rewrite rule is actually present in the live
  `coredns-config` Corefile; `"False"/CoreDNSRewriteRuleMissing` otherwise.
  (Kept, rather than removed, because unmodified GDC consumers and the e2e
  suites assert on it; it is now derived from real state instead of hardcoded.)

______________________________________________________________________

### 2.4 Reconciler 2: Multi-Network Services `Gateway` Translation

Because standard K8s Services use `eth0` (primary pod IPs) instead of the Multus
`net1` IPs, we must use a workaround via `externalIPs`.

Watch `Gateway`, `GKEGatewayCIDR`, `GKEL4Route`, and `GKEEndpointSelector`. Upon
reconciliation of a linked Gateway structure:

1. **Allocate IP:** Select an IP address from the matching `GKEGatewayCIDR`
   pool. **Current implementation note:** this is not a tracked allocation. The
   reconciler always returns the CIDR's first host address (network address +
   1). There is no bookkeeping of already-issued addresses and no exhaustion
   handling, so two `Gateway`s referencing the same `GKEGatewayCIDR` will be
   assigned the same VIP and collide. This is sufficient for today's
   one-Gateway-per-network test topology but must be fixed (real IPAM/allocation
   tracking) before supporting more than one Gateway per secondary network. A
   `Gateway.spec.addresses[0].value` set explicitly takes precedence over this
   derivation and skips allocation entirely.
2. **Select Bound Routes:** Only `GKEL4Route`s whose `spec.parentRefs` actually
   names this Gateway (and, when a `namespace` is set on the ref, matches the
   Gateway's namespace) are processed. Routes belonging to other Gateways in the
   same namespace are left untouched, including their status.
3. **Update GKEL4Route Status** (bound routes only):
   - Set `status.conditions`:
     - `Type: Accepted`, `Status: "True"`, `Reason: Accepted`
     - `Type: Ready`, `Status: "True"`, `Reason: Ready`
4. **Endpoint Scraping:** For every rule/backendRef across all bound routes,
   query the K8s API for Pods matching the referenced
   `GKEEndpointSelector.spec.selector`.
   - Only pods that are `Running` **and** have `PodReady: True` are included. A
     pod failing its readiness probe is excluded, matching standard Service
     endpoint semantics.
   - Secondary IPs come from the Multus `k8s.v1.cni.cncf.io/network-status`
     annotation, matching the entry's trailing name segment **exactly** against
     the target network (substring matching was a bug: `vlan-1` must not match
     `vlan-12`). The `networking.gke.io/pod-ips` annotation is not consulted.
     Nothing in GEM ever writes it.
   - Backend addresses are **aggregated (union, deduplicated)** across all
     rules/backendRefs before writing, so multi-route/multi-backend Gateways
     don't lose endpoints to last-write-wins.
5. **Dynamically Create Service:** Create one native Kubernetes `Service` object
   per Gateway.
   - Set `spec.clusterIP` to `None`.
   - Inject the allocated IP into **`spec.externalIPs`**. (This forces
     kube-proxy to load-balance traffic hitting this IP).
   - One `ServicePort` per unique backendRef port.
6. **Dynamically Create EndpointSlice:** Create a native `EndpointSlice` object
   linked to the dynamic Service, populated with the aggregated
   secondary-network IPs from step 4.
   - *Result: kube-proxy will DNAT externalIP traffic correctly out the VXLAN to
     the pod's `eth1`.*
7. **Update Gateway Status** (after routes/endpoints are reconciled):
   - Set `status.addresses: [{type: IPAddress, value: "<allocated_ip>"}]`.
   - Set `status.conditions`:
     - `Type: Accepted`, `Status: "True"`, `Reason: Accepted`
     - `Type: Programmed`, `Status: "True"`, `Reason: Programmed`,
       `Message: Gateway programmed; <N> ready backend endpoint(s)`. The
       ready-backend count makes an empty Gateway distinguishable from a healthy
       one in `.status`.
8. **DNS Masking (`.gkegw.cluster.local`):** Ensure the CoreDNS `coredns-config`
   (and `coredns-template`) contain the rewrite rule so
   `gateway-name.gateway-namespace.gkegw.cluster.local` resolves to the dynamic
   `externalIP`. **When (and only when) the Corefile is actually changed, the
   operator deletes the `kube-system` pods labeled `k8s-app=kube-dns`.** The
   stock ABM Corefile does carry the `reload` plugin, so CoreDNS would pick the
   change up within its reload interval regardless; deleting the pods simply
   makes it immediate. Idempotent no-op reconciles never bounce CoreDNS.

The Gateway controller requeues at a 30-second interval purely as a
drift-correction safety net. Watches on `Pod` (filtered to pods carrying
secondary-network annotations), `GKEL4Route`, and `GKEEndpointSelector` drive
event-based reconciliation.

______________________________________________________________________

### 2.5 Pod Interface Admission Mutating Webhook

To enable unmodified GDC Connected Pod manifests containing only
`networking.gke.io/interfaces` (without requiring manual Multus annotations),
`gem-network-operator` serves a **Mutating Admission Webhook** (`/mutate-pod`):

- **GDC Parity:** Intercepts `Pod` **CREATE** requests only. A pod updated later
  to add `networking.gke.io/interfaces` is never mutated.
- **Multus Translation:** Extracts secondary networks from
  `networking.gke.io/interfaces` and injects matching
  `k8s.v1.cni.cncf.io/networks` entries, merging with any the user already
  supplied.
- **Secondary IPAM:** For each secondary interface without an explicit address,
  the webhook allocates one and writes it into the Multus entry as
  `"ips": ["<addr>/<prefix>"]`. The allocator reads `gateway4` and
  `prefixLength4` from the `Network` and excludes the network and broadcast
  addresses, the gateway, the host node interface offsets, the
  `gdce-lb-service-vip-cidrs` ranges, every matching `GKEGatewayCIDR`, and all
  addresses already claimed in other pods' network annotations. Exhaustion
  raises an `IP pool exhausted` error.
- **Cilium Isolation:** Sanitizes `networking.gke.io/interfaces` so the
  underlying Anthos Cilium CNI only processes the primary `pod-network` on
  `eth0`, preventing broken Cilium IPAM allocator lookups on Anthos Bare Metal.
  Also sets `networking.gke.io/default-interface: eth0` when absent.
- **Resilience:** Operates with `failurePolicy: Ignore`,
  `matchPolicy: Equivalent`, `timeoutSeconds: 5`, and excludes system namespaces
  (`kube-system`, `gatekeeper-system`, `gke-system`). Self-signs CA and server
  certificates on every start, writing them to
  `/tmp/gem-network-operator-certs-<port>`, and registers the cluster-wide
  `gem-pod-interface-mutator` `MutatingWebhookConfiguration` with a URL-based
  `clientConfig`.

> [!WARNING]
> `failurePolicy: Ignore` means that if the operator is down or unreachable,
> pods are admitted **unmutated** and start with no secondary interface and no
> error anywhere. A pod missing `eth1` is the symptom.
>
> `EnsureMutatingWebhookConfiguration` overwrites the single cluster-wide
> webhook object with the calling instance's URL and CA, so with multiple
> operator instances the last writer wins.

Route installation itself is not the webhook's job. The `Network` reconciler
renders each `NetworkAttachmentDefinition` with the `macvlan` CNI plugin and
`host-local` IPAM, setting the `gateway` key to `Network.spec.gateway4` so the
CNI plugin installs the default route at pod initialization time. See §2.3.

______________________________________________________________________

### 2.6 Finalizer and Teardown Lifecycle Management

**Current implementation note:** only two finalizers are actually implemented;
there is no separate `GKEL4Route` finalizer.

- **`Gateway` Finalizer (`networking.gke.io/gateway-ip-protection`):**
  Reconciler deletes the dynamic `Service` and `EndpointSlice` before removing
  the finalizer. (There is no allocation pool to release back to, per the note
  in §2.4: VIP "allocation" is not tracked.)
- **`Network` Finalizer (`networking.gke.io/network-finalizer`):** On deletion,
  the reconciler explicitly deletes the child `NetworkAttachmentDefinition`s
  (across all namespaces) and the MetalLB `IPAddressPool`/`L2Advertisement`
  before removing the finalizer. All child objects also carry `OwnerReferences`
  to their `Network`, so Kubernetes garbage collection backs up the explicit
  cleanup. (`Network` deletion mid-cluster-life is still not a supported
  operation. This is defense-in-depth; cluster teardown remains
  `ansible/cleanup.yaml`'s job.)
- `GKEL4Route` and `GKEEndpointSelector` have no finalizers. They are treated as
  pure inputs read by the `Gateway` reconciler, not owned resources with their
  own cleanup lifecycle.

> [!NOTE]
> Physical GDC Connected **does** put a finalizer on `GKEL4Route`, which is why
> the teardown ordering rule in
> [Secondary Networks](secondary-networks.md#delete-routes-before-their-parents)
> exists. GEM does not implement it, so the finalizer deadlock cannot occur
> here. Manifests shared with physical GDC should still follow the ordering.

### 2.7 Service → MetalLB Binding (GEM-only alternate path)

Separately from the Gateway API flow above,
`NetworkReconciler.reconcileServices` watches all `Service`s. Any `Service`
annotated `networking.gke.io/network: <network-name>` gets
`metallb.universe.tf/address-pool: <network-name>-pool` applied automatically,
binding it to that network's MetalLB `IPAddressPool`. This is unit-tested
(`network_controller_test.go`) but not exercised by any e2e suite and not part
of the GDC-compatible CRD surface. It is a GEM-only convenience for binding a
plain `Service` (as opposed to a Gateway-fronted one) to a secondary network's
VIP pool.

**Namespace allowlist:** because secondary networks are admin-curated isolation
boundaries while Services are freely user-created, a `Network` may restrict
which namespaces can use this binding via the
`networking.gke.io/gdce-allowed-namespaces` annotation (comma-separated
namespace names, or `*`). When the annotation is **absent, empty, or `*`, all
namespaces are allowed**, the historical open behavior, so existing clusters are
unaffected until an operator opts in. A denied binding is skipped (the Service
is left unbound), logged, and surfaced as a `Warning` event
(`ServiceBindingDenied`) on the `Network`.

### 2.8 Known Gaps / Non-Functional Paths

- **`ClusterCIDRConfig` reconciliation was removed.** Earlier versions created
  `networking.gke.io/v1alpha1 ClusterCIDRConfig` objects for which no CRD was
  ever installed, so the create failed silently on every reconcile. The path
  (and its `gke-gateway-pod-cidr`/`gdce-per-node-ipam-size` plumbing in the
  operator) was deleted rather than made real; the annotations remain
  accepted-but-ignored on `Network` CRs for GDC-manifest parity. Secondary pod
  IPAM is actually provided by the NAD's `host-local` configuration (§2.3).
- **`host-local` IPAM is not partitioned per node.** The generated NAD sets only
  `subnet` and `gateway`, with no `rangeStart`/`rangeEnd` and no exclusion of
  the node host addresses or the MetalLB VIP range. Each node maintains an
  independent allocation store over the whole subnet, so pods on different nodes
  can receive the same address, and a pod can receive a node's own
  `gdcenet0.<vlan>` address. The webhook's own allocator (§2.5) computes a
  conflict-free address and writes it as `ips`, but the NAD does not declare
  `capabilities: {"ips": true}`, so Multus may disregard the request and fall
  back to plain `host-local` allocation.
- **Gateway VIP assignment is not allocation.** `determineGatewayIP` always
  returns the first host address of the matching `GKEGatewayCIDR`, with no
  tracking of issued addresses and no exhaustion handling, so two `Gateway`s on
  one CIDR collide. This behavior is pinned by
  `TestGatewayReconciler_Reconcile_TwoGatewaysSameCIDRCollide`.

______________________________________________________________________

## Phase 3: Verification and E2E Validation

Validation must be executed against the Chainsaw E2E suites located in
`tests/e2e/secondary-networks/`:

1. **Intra-Node Suite** (`tests/e2e/secondary-networks/intra-node/`):
   - Validates local pod-to-pod reachability, Gateway API DNS resolution
     (`*.gkegw.cluster.local`), and LoadBalancer VIP allocation using
     `podAffinity`.
   - Run: `chainsaw test tests/e2e/secondary-networks/intra-node`
2. **Cross-Node Suite** (`tests/e2e/secondary-networks/cross-node/`):
   - Validates multi-node VXLAN encapsulation, cross-host Gateway routing, and
     multi-node load balancing using `podAntiAffinity`.
   - Run: `chainsaw test tests/e2e/secondary-networks/cross-node`
3. **Pod Mutator Suite** (`tests/e2e/secondary-networks/pod-mutator/`):
   - Validates the admission webhook end to end: the injected `ips` key in
     `k8s.v1.cni.cncf.io/networks` and the sanitized
     `networking.gke.io/interfaces` annotation.
   - Run: `chainsaw test tests/e2e/secondary-networks/pod-mutator`
