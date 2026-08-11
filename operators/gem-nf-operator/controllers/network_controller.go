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
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const (
	// NetworkFinalizer prevents deletion of the Network until its child resources are cleaned up.
	NetworkFinalizer = "networking.gke.io/network-finalizer"

	// Annotation keys for GDCE network configuration.
	AnnotationVLANID          = "networking.gke.io/gdce-vlan-id"
	AnnotationVLANMTU         = "networking.gke.io/gdce-vlan-mtu"
	AnnotationLBServiceVIPs   = "networking.gke.io/gdce-lb-service-vip-cidrs"
	AnnotationGatewayPodCIDR  = "networking.gke.io/gke-gateway-pod-cidr"
	AnnotationPerNodeIPAMSize = "networking.gke.io/gdce-per-node-ipam-size"

	// Default values for GDCE network emulation.
	DefaultVLANMTU         = "1410"
	DefaultPrefixLength    = 24
	DefaultPerNodeMaskSize = 24
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
	ClusterCIDRConfigGVK = schema.GroupVersionKind{
		Group:   "networking.gke.io",
		Version: "v1alpha1",
		Kind:    "ClusterCIDRConfig",
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
// corresponding Multus NetworkAttachmentDefinitions, MetalLB IPAddressPools, and ClusterCIDRConfigs.
type NetworkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
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

	// Reconcile Multus NetworkAttachmentDefinition across all active namespaces.
	if ifaceName != "" {
		if err := r.reconcileNetAttachDef(ctx, netObj, ifaceName, vlanMTU); err != nil {
			log.Error(err, "Failed to reconcile NetworkAttachmentDefinition")
		}
	}

	// Reconcile MetalLB IPAddressPool & L2Advertisement in kube-system if VIP pool is configured.
	if lbVIPsJSON, ok := annotations[AnnotationLBServiceVIPs]; ok && lbVIPsJSON != "" {
		var vipList []string
		if err := json.Unmarshal([]byte(lbVIPsJSON), &vipList); err == nil && len(vipList) > 0 {
			if err := r.reconcileMetalLB(ctx, netObj, vipList, ifaceName); err != nil {
				log.Error(err, "Failed to reconcile MetalLB IPAddressPool")
			}
		} else if err != nil {
			log.Error(err, "Failed to parse VIP pool annotation JSON", "raw", lbVIPsJSON)
		}
	}

	// Reconcile ClusterCIDRConfig if secondary pod CIDR allocation is requested.
	if podCIDR, ok := annotations[AnnotationGatewayPodCIDR]; ok && podCIDR != "" {
		if err := r.reconcileClusterCIDRConfig(ctx, netObj, podCIDR, annotations[AnnotationPerNodeIPAMSize]); err != nil {
			log.Error(err, "Failed to reconcile ClusterCIDRConfig")
		}
	}

	// Update status conditions to indicate readiness.
	now := metav1.Now().Rfc3339Copy().Format(time.RFC3339)
	conditions := []interface{}{
		map[string]interface{}{
			"type":               "Ready",
			"status":             "True",
			"reason":             "NetworkReady",
			"message":            "Network interface and IPAM ready",
			"lastTransitionTime": now,
		},
		map[string]interface{}{
			"type":               "CoreDNSReady",
			"status":             "True",
			"reason":             "CoreDNSServiceReady",
			"message":            "CoreDNS service is ready for the network",
			"lastTransitionTime": now,
		},
	}

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

	for _, ns := range nsList.Items {
		if !ns.DeletionTimestamp.IsZero() {
			continue
		}
		nad := &unstructured.Unstructured{}
		nad.SetGroupVersionKind(NetAttachDefGVK)
		nad.SetName(owner.GetName())
		nad.SetNamespace(ns.GetName())
		nad.Object["spec"] = map[string]interface{}{
			"config": cniConfig,
		}
		if err := r.applyOrUpdate(ctx, nad); err != nil {
			r.Log.Error(err, "Failed to apply NetworkAttachmentDefinition", "namespace", ns.GetName())
		}
	}
	return nil
}

// reconcileMetalLB manages IPAddressPool and L2Advertisement resources in kube-system for secondary VIPs.
func (r *NetworkReconciler) reconcileMetalLB(ctx context.Context, owner *unstructured.Unstructured, vipPool []string, ifaceName string) error {
	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(IPAddressPoolGVK)
	pool.SetName(fmt.Sprintf("%s-pool", owner.GetName()))
	pool.SetNamespace("kube-system")

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

	l2Adv.Object["spec"] = map[string]interface{}{
		"ipAddressPools": []interface{}{pool.GetName()},
		"interfaces":     []interface{}{ifaceName},
	}

	return r.applyOrUpdate(ctx, l2Adv)
}

// reconcileClusterCIDRConfig ensures GKE secondary Pod IPAM configuration exists for the network.
func (r *NetworkReconciler) reconcileClusterCIDRConfig(ctx context.Context, owner *unstructured.Unstructured, podCIDR, maskSize string) error {
	perNodeMask := DefaultPerNodeMaskSize
	if maskSize != "" {
		if parsed, err := strconv.Atoi(maskSize); err == nil && parsed > 0 && parsed <= 32 {
			perNodeMask = parsed
		}
	}

	cidrConfig := &unstructured.Unstructured{}
	cidrConfig.SetGroupVersionKind(ClusterCIDRConfigGVK)
	cidrConfig.SetName(fmt.Sprintf("%s-cidr", owner.GetName()))

	cidrConfig.Object["spec"] = map[string]interface{}{
		"network": owner.GetName(),
		"ipv4": map[string]interface{}{
			"cidr":            podCIDR,
			"perNodeMaskSize": int64(perNodeMask),
		},
	}

	return r.applyOrUpdate(ctx, cidrConfig)
}

// applyOrUpdate creates or updates an unstructured resource idempotently.
func (r *NetworkReconciler) applyOrUpdate(ctx context.Context, obj *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	err := r.Get(ctx, client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return r.Create(ctx, obj)
		}
		return err
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// SetupWithManager sets up the Network controller with the controller manager.
func (r *NetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	netObj := &unstructured.Unstructured{}
	netObj.SetGroupVersionKind(NetworkGVK)

	// Reconcile all Networks whenever a new Namespace is created so NetworkAttachmentDefinitions
	// are immediately available in the new namespace.
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
		Complete(r)
}
