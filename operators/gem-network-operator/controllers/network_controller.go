// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const (
	// NetworkFinalizer prevents deletion of the Network until its child resources are cleaned up.
	NetworkFinalizer = "networking.gke.io/network-finalizer"

	// Annotation keys for GEM network configuration.
	AnnotationNetworkTarget = "networking.gke.io/network"
	AnnotationVLANID        = "networking.gke.io/gdce-vlan-id"
	AnnotationVLANMTU       = "networking.gke.io/gdce-vlan-mtu"
	AnnotationLBServiceVIPs = "networking.gke.io/gdce-lb-service-vip-cidrs"

	// AnnotationAllowedNamespaces (on a Network) restricts which namespaces' Services may bind to
	// the network's MetalLB pool via the networking.gke.io/network annotation. Comma-separated
	// namespace names, or "*" for all. Absent means all namespaces are allowed (GEM's historical
	// open behavior), so existing clusters are unaffected until an operator opts in.
	AnnotationAllowedNamespaces = "networking.gke.io/gdce-allowed-namespaces"

	// Default values for GEM network emulation.
	DefaultVLANMTU      = "1410"
	DefaultPrefixLength = 24

	// ProvisionedNetworksConfigMap is published to kube-system by Ansible at cluster build time and
	// enumerates the VLANs whose host VXLAN interfaces were statically provisioned (key: vlan-id,
	// value: interface name). Networks not listed there have no functioning host data plane.
	ProvisionedNetworksConfigMap = "gem-provisioned-networks"
)

var (
	NetworkGVK = schema.GroupVersionKind{
		Group:   "networking.gke.io",
		Version: "v1",
		Kind:    "Network",
	}
	NetAttachDefGVK = schema.GroupVersionKind{
		Group:   "k8s.cni.cncf.io",
		Version: "v1",
		Kind:    "NetworkAttachmentDefinition",
	}
	IPAddressPoolGVK = schema.GroupVersionKind{
		Group:   "metallb.io",
		Version: "v1beta1",
		Kind:    "IPAddressPool",
	}
	L2AdvertisementGVK = schema.GroupVersionKind{
		Group:   "metallb.io",
		Version: "v1beta1",
		Kind:    "L2Advertisement",
	}
)

// NetworkReconciler reconciles networking.gke.io Network custom resources by dynamically provisioning
// corresponding Multus NetworkAttachmentDefinitions and MetalLB IPAddressPools/L2Advertisements.
type NetworkReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder
}

// Reconcile processes a Network custom resource.
func (r *NetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("network", req.Name)

	netObj := &unstructured.Unstructured{}
	netObj.SetGroupVersionKind(NetworkGVK)

	if err := r.Get(ctx, req.NamespacedName, netObj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle resource deletion and finalizer cleanup.
	if !netObj.GetDeletionTimestamp().IsZero() {
		if controllerutil.ContainsFinalizer(netObj, NetworkFinalizer) {
			log.Info("Cleaning up resources for Network", "name", netObj.GetName())
			r.cleanupNetworkChildren(ctx, netObj)
			controllerutil.RemoveFinalizer(netObj, NetworkFinalizer)
			if err := r.Update(ctx, netObj); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer is present.
	if !controllerutil.ContainsFinalizer(netObj, NetworkFinalizer) {
		controllerutil.AddFinalizer(netObj, NetworkFinalizer)
		if err := r.Update(ctx, netObj); err != nil {
			return ctrl.Result{}, err
		}
	}

	annotations := netObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	vlanID := annotations[AnnotationVLANID]
	vlanMTU := annotations[AnnotationVLANMTU]
	if vlanMTU == "" {
		vlanMTU = DefaultVLANMTU
	}

	spec, _, _ := unstructured.NestedMap(netObj.Object, "spec")
	if spec == nil {
		spec = make(map[string]interface{})
	}

	ifaceName, _, _ := unstructured.NestedString(spec, "nodeInterfaceMatcher", "interfaceName")
	if ifaceName == "" && vlanID != "" {
		ifaceName = fmt.Sprintf("gdcenet0.%s", vlanID)
	}

	// Guardrail: secondary networks only function if Ansible statically provisioned the matching
	// host VXLAN interface at cluster build time. A Network applied afterwards is accepted (child
	// resources are still created, per the GDC contract) but must be loudly diagnosable, not
	// silently "Ready": pods referencing it will fail Multus ADD because no host interface exists.
	// The cluster's primary network (e.g. pod-network / default) is managed by the base CNI and
	// is always provisioned.
	isPrimary := isPrimaryNetwork(netObj, vlanID, ifaceName)
	provisioned := isPrimary || r.isHostInterfaceProvisioned(ctx, vlanID, ifaceName)
	if !provisioned && r.Recorder != nil {
		r.Recorder.Event(netObj, corev1.EventTypeWarning, "MissingHostInterface",
			"Network accepted, but the underlying host VXLAN was not statically provisioned. "+
				"Pods attempting to use this network will fail. Rebuild cluster using Ansible variables to activate.")
	}

	// Child-resource reconcile failures are collected so the Ready condition reflects them
	// instead of reporting an unconditional success.
	var childErrors []string

	// Reconcile Multus NetworkAttachmentDefinition across all active namespaces.
	if ifaceName != "" {
		if err := r.reconcileNetAttachDef(ctx, netObj, ifaceName, vlanMTU); err != nil {
			log.Error(err, "Failed to reconcile NetworkAttachmentDefinition")
			childErrors = append(childErrors, fmt.Sprintf("NetworkAttachmentDefinition: %v", err))
		}
	}

	// Reconcile MetalLB IPAddressPool & L2Advertisement in kube-system if VIP pool is configured.
	if lbVIPsJSON, ok := annotations[AnnotationLBServiceVIPs]; ok && lbVIPsJSON != "" {
		var vipList []string
		if err := json.Unmarshal([]byte(lbVIPsJSON), &vipList); err == nil && len(vipList) > 0 {
			if err := r.reconcileMetalLB(ctx, netObj, vipList, ifaceName); err != nil {
				log.Error(err, "Failed to reconcile MetalLB IPAddressPool")
				childErrors = append(childErrors, fmt.Sprintf("IPAddressPool/L2Advertisement: %v", err))
			}
		} else if err != nil {
			log.Error(err, "Failed to parse VIP pool annotation JSON", "raw", lbVIPsJSON)
			childErrors = append(childErrors, fmt.Sprintf("VIP pool annotation: %v", err))
		}
	}

	// Reconcile Services requesting this secondary network to bind to the MetalLB address pool.
	if err := r.reconcileServices(ctx, netObj); err != nil {
		log.Error(err, "Failed to reconcile secondary network Services")
		childErrors = append(childErrors, fmt.Sprintf("Service binding: %v", err))
	}

	// Update status conditions to reflect actual readiness.
	now := metav1.Now().Rfc3339Copy().Format(time.RFC3339)
	readyCond := map[string]interface{}{
		"type":               "Ready",
		"status":             "True",
		"reason":             "NetworkReady",
		"message":            "Network interface and IPAM ready",
		"lastTransitionTime": now,
	}
	switch {
	case !provisioned:
		readyCond["status"] = "False"
		readyCond["reason"] = "MissingHostInterface"
		readyCond["message"] = "Network accepted, but the underlying host VXLAN was not statically provisioned. " +
			"Pods attempting to use this network will fail. Rebuild cluster using Ansible variables to activate."
	case len(childErrors) > 0:
		readyCond["status"] = "False"
		readyCond["reason"] = "ChildResourceError"
		readyCond["message"] = "Failed to reconcile child resources: " + strings.Join(childErrors, "; ")
	}

	// CoreDNSReady reflects whether the .gkegw.cluster.local rewrite rule is actually active in
	// CoreDNS — the condition unmodified GDC consumers and the e2e suites assert on.
	coreDNSCond := map[string]interface{}{
		"type":               "CoreDNSReady",
		"status":             "True",
		"reason":             "CoreDNSServiceReady",
		"message":            "CoreDNS service is ready for the network",
		"lastTransitionTime": now,
	}
	if !r.isCoreDNSGatewayRuleActive(ctx) {
		coreDNSCond["status"] = "False"
		coreDNSCond["reason"] = "CoreDNSRewriteRuleMissing"
		coreDNSCond["message"] = "CoreDNS is not configured with the .gkegw.cluster.local rewrite rule"
	}

	conditions := []interface{}{readyCond, coreDNSCond}

	statusMap := map[string]interface{}{
		"conditions": conditions,
	}

	if err := unstructured.SetNestedField(netObj.Object, statusMap, "status"); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.Status().Update(ctx, netObj); err != nil {
		_ = r.Update(ctx, netObj)
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// networkOwnerReference builds an OwnerReference to the (cluster-scoped) Network for its child
// objects, so ownership is visible and Kubernetes garbage collection backs up the finalizer.
func networkOwnerReference(netObj *unstructured.Unstructured) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: netObj.GetAPIVersion(),
		Kind:       netObj.GetKind(),
		Name:       netObj.GetName(),
		UID:        netObj.GetUID(),
		Controller: &controller,
	}
}

// cleanupNetworkChildren deletes the child objects created for a Network: its
// NetworkAttachmentDefinitions across all namespaces and the MetalLB
// IPAddressPool/L2Advertisement. NotFound errors are ignored.
func (r *NetworkReconciler) cleanupNetworkChildren(ctx context.Context, netObj *unstructured.Unstructured) {
	name := netObj.GetName()

	nsList := &corev1.NamespaceList{}
	if err := r.List(ctx, nsList); err == nil {
		for _, ns := range nsList.Items {
			nad := &unstructured.Unstructured{}
			nad.SetGroupVersionKind(NetAttachDefGVK)
			nad.SetName(name)
			nad.SetNamespace(ns.GetName())
			if err := r.Delete(ctx, nad); client.IgnoreNotFound(err) != nil {
				r.Log.Error(err, "Failed to delete NetworkAttachmentDefinition", "namespace", ns.GetName(), "network", name)
			}
		}
	} else {
		r.Log.Error(err, "Failed to list namespaces during Network cleanup", "network", name)
	}

	for _, child := range []struct {
		gvk       schema.GroupVersionKind
		namespace string
		name      string
	}{
		{IPAddressPoolGVK, "kube-system", fmt.Sprintf("%s-pool", name)},
		{L2AdvertisementGVK, "kube-system", fmt.Sprintf("l2advertise-%s", name)},
	} {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(child.gvk)
		obj.SetName(child.name)
		obj.SetNamespace(child.namespace)
		if err := r.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "Failed to delete Network child resource", "kind", child.gvk.Kind, "name", child.name, "network", name)
		}
	}
}

// isCoreDNSGatewayRuleActive reports whether the .gkegw.cluster.local rewrite rule is present in
// the live CoreDNS Corefile (injected by Ansible at build time and kept by the Gateway reconciler).
func (r *NetworkReconciler) isCoreDNSGatewayRuleActive(ctx context.Context) bool {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "coredns-config"}, cm); err != nil {
		return false
	}
	return strings.Contains(cm.Data["Corefile"], ".gkegw.cluster.local")
}

// isHostInterfaceProvisioned checks the Network's VLAN or interface name against the
// gem-provisioned-networks ConfigMap that Ansible publishes at cluster build time.
func (r *NetworkReconciler) isHostInterfaceProvisioned(ctx context.Context, vlanID, ifaceName string) bool {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: ProvisionedNetworksConfigMap}, cm); err != nil {
		return false
	}
	if vlanID != "" {
		if _, ok := cm.Data[vlanID]; ok {
			return true
		}
	}
	if ifaceName != "" {
		for _, provisionedIface := range cm.Data {
			if provisionedIface == ifaceName {
				return true
			}
		}
	}
	return false
}

// isPrimaryNetwork reports whether a Network represents the default cluster primary network
// (e.g. "pod-network" or "default") rather than an administrator-defined secondary network segment.
func isPrimaryNetwork(netObj *unstructured.Unstructured, vlanID, ifaceName string) bool {
	name := netObj.GetName()
	if name == "pod-network" || name == "default" {
		return true
	}
	return vlanID == "" && ifaceName == ""
}

// reconcileNetAttachDef generates a NetworkAttachmentDefinition in every active namespace,
// configuring the macvlan CNI and host-local IPAM using the Network spec's gateway and prefix.
func (r *NetworkReconciler) reconcileNetAttachDef(ctx context.Context, owner *unstructured.Unstructured, ifaceName, mtu string) error {
	gateway4, _, _ := unstructured.NestedString(owner.Object, "spec", "gateway4")
	prefixLen, found, _ := unstructured.NestedInt64(owner.Object, "spec", "l2NetworkConfig", "prefixLength4")
	if !found || prefixLen <= 0 || prefixLen > 32 {
		prefixLen = DefaultPrefixLength
	}

	var ipamConfig string
	if gateway4 != "" {
		ip := net.ParseIP(gateway4)
		if ip != nil && ip.To4() != nil {
			mask := net.CIDRMask(int(prefixLen), 32)
			netIP := ip.To4().Mask(mask)
			subnetCIDR := fmt.Sprintf("%s/%d", netIP.String(), prefixLen)
			ipamConfig = fmt.Sprintf(`{
    "type": "host-local",
    "subnet": "%s",
    "gateway": "%s"
  }`, subnetCIDR, gateway4)
		}
	}
	if ipamConfig == "" {
		ipamConfig = `{
    "type": "host-local",
    "subnet": "usePodCidr"
  }`
	}

	cniConfig := fmt.Sprintf(`{
  "cniVersion": "0.3.1",
  "type": "macvlan",
  "master": "%s",
  "mode": "bridge",
  "mtu": %s,
  "ipam": %s
}`, ifaceName, mtu, ipamConfig)

	nsList := &corev1.NamespaceList{}
	if err := r.List(ctx, nsList); err != nil {
		return err
	}

	var applyErrors []error
	for _, ns := range nsList.Items {
		if !ns.DeletionTimestamp.IsZero() {
			continue
		}
		nad := &unstructured.Unstructured{}
		nad.SetGroupVersionKind(NetAttachDefGVK)
		nad.SetName(owner.GetName())
		nad.SetNamespace(ns.GetName())
		nad.SetOwnerReferences([]metav1.OwnerReference{networkOwnerReference(owner)})
		nad.Object["spec"] = map[string]interface{}{
			"config": cniConfig,
		}
		if err := r.applyOrUpdate(ctx, nad); err != nil {
			r.Log.Error(err, "Failed to apply NetworkAttachmentDefinition", "namespace", ns.GetName())
			applyErrors = append(applyErrors, fmt.Errorf("namespace %s: %w", ns.GetName(), err))
		}
	}
	return errors.Join(applyErrors...)
}

// reconcileMetalLB manages IPAddressPool and L2Advertisement resources in kube-system for secondary VIPs.
func (r *NetworkReconciler) reconcileMetalLB(ctx context.Context, owner *unstructured.Unstructured, vipPool []string, ifaceName string) error {
	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(IPAddressPoolGVK)
	pool.SetName(fmt.Sprintf("%s-pool", owner.GetName()))
	pool.SetNamespace("kube-system")
	pool.SetOwnerReferences([]metav1.OwnerReference{networkOwnerReference(owner)})

	var addresses []interface{}
	for _, vip := range vipPool {
		addresses = append(addresses, vip)
	}

	pool.Object["spec"] = map[string]interface{}{
		"addresses":  addresses,
		"autoAssign": false,
	}

	if err := r.applyOrUpdate(ctx, pool); err != nil {
		return err
	}

	l2Adv := &unstructured.Unstructured{}
	l2Adv.SetGroupVersionKind(L2AdvertisementGVK)
	l2Adv.SetName(fmt.Sprintf("l2advertise-%s", owner.GetName()))
	l2Adv.SetNamespace("kube-system")
	l2Adv.SetOwnerReferences([]metav1.OwnerReference{networkOwnerReference(owner)})

	l2Adv.Object["spec"] = map[string]interface{}{
		"ipAddressPools": []interface{}{pool.GetName()},
		"interfaces":     []interface{}{ifaceName},
	}

	return r.applyOrUpdate(ctx, l2Adv)
}

// namespaceAllowedForNetwork evaluates the Network's gdce-allowed-namespaces annotation.
// An absent or "*" annotation allows every namespace (the historical open behavior).
func namespaceAllowedForNetwork(netObj *unstructured.Unstructured, namespace string) bool {
	allowlist, ok := netObj.GetAnnotations()[AnnotationAllowedNamespaces]
	if !ok || strings.TrimSpace(allowlist) == "" || strings.TrimSpace(allowlist) == "*" {
		return true
	}
	for _, allowed := range strings.Split(allowlist, ",") {
		if strings.TrimSpace(allowed) == namespace {
			return true
		}
	}
	return false
}

// reconcileServices binds Services annotated with networking.gke.io/network to the corresponding
// MetalLB pool, subject to the Network's namespace allowlist: secondary networks are admin-curated
// isolation boundaries, so a Service must not be able to join one from a namespace the Network's
// owner didn't permit.
func (r *NetworkReconciler) reconcileServices(ctx context.Context, netObj *unstructured.Unstructured) error {
	svcList := &corev1.ServiceList{}
	if err := r.List(ctx, svcList); err != nil {
		return err
	}

	networkName := netObj.GetName()
	targetPool := fmt.Sprintf("%s-pool", networkName)
	for _, svc := range svcList.Items {
		if netName, ok := svc.Annotations[AnnotationNetworkTarget]; ok && netName == networkName {
			if !namespaceAllowedForNetwork(netObj, svc.Namespace) {
				r.Log.Info("Refusing to bind Service to network: namespace not in allowlist",
					"service", svc.Name, "namespace", svc.Namespace, "network", networkName)
				if r.Recorder != nil {
					r.Recorder.Eventf(netObj, corev1.EventTypeWarning, "ServiceBindingDenied",
						"Service %s/%s requested this network but namespace %q is not in the %s allowlist",
						svc.Namespace, svc.Name, svc.Namespace, AnnotationAllowedNamespaces)
				}
				continue
			}
			if svc.Annotations == nil {
				svc.Annotations = make(map[string]string)
			}
			if svc.Annotations["metallb.universe.tf/address-pool"] != targetPool {
				svc.Annotations["metallb.universe.tf/address-pool"] = targetPool
				if err := r.Update(ctx, &svc); err != nil {
					r.Log.Error(err, "Failed to bind Service to MetalLB address pool", "service", svc.Name, "namespace", svc.Namespace)
				}
			}
		}
	}
	return nil
}

// applyOrUpdate creates or updates an unstructured resource idempotently, skipping the write
// entirely when the desired spec already matches — Network specs are effectively immutable after
// cluster build, so steady-state reconciles would otherwise rewrite every child object for nothing.
// Updates retry on optimistic-concurrency conflicts (e.g. Ansible re-applying manifests, manual
// kubectl edits) with a freshly read ResourceVersion.
func (r *NetworkReconciler) applyOrUpdate(ctx context.Context, obj *unstructured.Unstructured) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(obj.GroupVersionKind())
		err := r.Get(ctx, client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}, existing)
		if err != nil {
			if client.IgnoreNotFound(err) == nil {
				return r.Create(ctx, obj)
			}
			return err
		}

		if reflect.DeepEqual(existing.Object["spec"], obj.Object["spec"]) {
			return nil
		}

		obj.SetResourceVersion(existing.GetResourceVersion())
		return r.Update(ctx, obj)
	})
}

// SetupWithManager sets up the Network controller with the controller manager.
func (r *NetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	netObj := &unstructured.Unstructured{}
	netObj.SetGroupVersionKind(NetworkGVK)

	// Reconcile all Networks whenever a new Namespace is created so NetworkAttachmentDefinitions
	// are immediately available in the new namespace, or whenever a Service requests secondary networking.
	return ctrl.NewControllerManagedBy(mgr).
		For(netObj).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []ctrl.Request {
			netList := &unstructured.UnstructuredList{}
			netList.SetGroupVersionKind(NetworkGVK)
			if err := r.List(ctx, netList); err != nil {
				return nil
			}
			var reqs []ctrl.Request
			for _, item := range netList.Items {
				reqs = append(reqs, ctrl.Request{
					NamespacedName: client.ObjectKey{Name: item.GetName()},
				})
			}
			return reqs
		})).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []ctrl.Request {
			if netName, ok := o.GetAnnotations()[AnnotationNetworkTarget]; ok && netName != "" {
				return []ctrl.Request{
					{NamespacedName: client.ObjectKey{Name: netName}},
				}
			}
			return nil
		})).
		Complete(r)
}
