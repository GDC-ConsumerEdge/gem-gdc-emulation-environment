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
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const (
	// GatewayFinalizer ensures dynamic services and endpoint slices are deleted when the Gateway is deleted.
	GatewayFinalizer = "networking.gke.io/gateway-ip-protection"

	// AnnotationNetwork specifies the target networking.gke.io Network for the Gateway.
	AnnotationNetwork = "networking.gke.io/network"

	// AnnotationPodIPs contains JSON-encoded secondary IP assignments on pods.
	AnnotationPodIPs = "networking.gke.io/pod-ips"

	// AnnotationNetworkStatus contains Multus CNI network status JSON on pods.
	AnnotationNetworkStatus = "k8s.v1.cni.cncf.io/network-status"

	// CoreDNS rewrite rule bridging GDC Gateway DNS (*.gkegw.cluster.local) to Kubernetes Service DNS.
	CoreDNSRewriteRule = "    rewrite name suffix .gkegw.cluster.local .svc.cluster.local"
)

var (
	GatewayGVK = schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "Gateway",
	}
	GKEGatewayCIDRGVK = schema.GroupVersionKind{
		Group:   "networking.gke.io",
		Version: "v1",
		Kind:    "GKEGatewayCIDR",
	}
	GKEL4RouteGVK = schema.GroupVersionKind{
		Group:   "networking.gke.io",
		Version: "v1",
		Kind:    "GKEL4Route",
	}
	GKEEndpointSelectorGVK = schema.GroupVersionKind{
		Group:   "networking.gke.io",
		Version: "v1",
		Kind:    "GKEEndpointSelector",
	}
)

// PodIPEntry represents a secondary network IP assignment from the networking.gke.io/pod-ips annotation.
type PodIPEntry struct {
	NetworkName string `json:"networkName"`
	IP          string `json:"ip"`
}

// GatewayReconciler reconciles Gateway, GKEGatewayCIDR, GKEL4Route, and GKEEndpointSelector resources.
// It bridges secondary Multus network endpoints into standard Kubernetes Gateway routing by dynamically
// provisioning headless Services and custom EndpointSlices containing secondary Pod IPs.
type GatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// Reconcile handles Gateway lifecycle events.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gateway", req.NamespacedName)

	gw := &unstructured.Unstructured{}
	gw.SetGroupVersionKind(GatewayGVK)

	if err := r.Get(ctx, req.NamespacedName, gw); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion and cleanup of dynamic child resources.
	if !gw.GetDeletionTimestamp().IsZero() {
		if controllerutil.ContainsFinalizer(gw, GatewayFinalizer) {
			r.cleanupDynamicResources(ctx, gw)
			controllerutil.RemoveFinalizer(gw, GatewayFinalizer)
			if err := r.Update(ctx, gw); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer is present.
	if !controllerutil.ContainsFinalizer(gw, GatewayFinalizer) {
		controllerutil.AddFinalizer(gw, GatewayFinalizer)
		if err := r.Update(ctx, gw); err != nil {
			return ctrl.Result{}, err
		}
	}

	targetNetwork := gw.GetAnnotations()[AnnotationNetwork]
	if targetNetwork == "" {
		log.Info("Gateway missing networking.gke.io/network annotation, waiting for configuration")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Resolve the Gateway VIP from static spec addresses or dynamically from GKEGatewayCIDR.
	gwIP := r.determineGatewayIP(ctx, gw, targetNetwork)
	if gwIP == "" {
		log.Info("Waiting for GKEGatewayCIDR to allocate Gateway VIP", "network", targetNetwork)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Update Gateway status addresses and conditions to reflect Accepted and Programmed state.
	now := metav1.Now().Rfc3339Copy().Format(time.RFC3339)
	addresses := []interface{}{
		map[string]interface{}{
			"type":  "IPAddress",
			"value": gwIP,
		},
	}
	conditions := []interface{}{
		map[string]interface{}{
			"type":               "Accepted",
			"status":             "True",
			"reason":             "Accepted",
			"message":            "Gateway accepted by networking.gke.io/cluster-ip",
			"lastTransitionTime": now,
		},
		map[string]interface{}{
			"type":               "Programmed",
			"status":             "True",
			"reason":             "Programmed",
			"message":            "Gateway programmed and IP allocation prepared",
			"lastTransitionTime": now,
		},
	}

	statusMap := map[string]interface{}{
		"addresses":  addresses,
		"conditions": conditions,
	}

	_ = unstructured.SetNestedField(gw.Object, statusMap, "status")
	if err := r.Status().Update(ctx, gw); err != nil {
		_ = r.Update(ctx, gw)
	}

	// Dynamically manage Services and EndpointSlices for attached GKEL4Routes and GKEEndpointSelectors.
	r.reconcileRoutesAndEndpoints(ctx, gw, gwIP, targetNetwork)

	// Ensure CoreDNS rewrites *.gkegw.cluster.local to *.svc.cluster.local for Gateway service discovery.
	r.reconcileCoreDNS(ctx)

	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

// determineGatewayIP extracts a static address from Gateway spec or allocates the first usable IP
// from matching GKEGatewayCIDR resources.
func (r *GatewayReconciler) determineGatewayIP(ctx context.Context, gw *unstructured.Unstructured, networkName string) string {
	// Check for static IP specified in Gateway spec.addresses.
	specAddresses, found, _ := unstructured.NestedSlice(gw.Object, "spec", "addresses")
	if found && len(specAddresses) > 0 {
		if addrMap, ok := specAddresses[0].(map[string]interface{}); ok {
			if val, ok := addrMap["value"].(string); ok && val != "" {
				return val
			}
		}
	}

	if networkName == "" {
		return ""
	}

	// Search for matching GKEGatewayCIDR.
	cidrList := &unstructured.UnstructuredList{}
	cidrList.SetGroupVersionKind(GKEGatewayCIDRGVK)
	if err := r.List(ctx, cidrList); err == nil {
		for _, item := range cidrList.Items {
			netName, _, _ := unstructured.NestedString(item.Object, "spec", "network")
			if netName == networkName || item.GetName() == networkName {
				ip4cidr, _, _ := unstructured.NestedString(item.Object, "spec", "ip4cidr")
				if ip4cidr != "" {
					_, ipNet, err := net.ParseCIDR(ip4cidr)
					if err == nil && ipNet != nil {
						baseIP := ipNet.IP.To4()
						if baseIP != nil {
							// Allocate the first usable host IP in the CIDR block (network address + 1).
							allocIP := make(net.IP, len(baseIP))
							copy(allocIP, baseIP)
							allocIP[3]++
							if ipNet.Contains(allocIP) {
								return allocIP.String()
							}
						}
					}
				}
			}
		}
	}

	return ""
}

// reconcileRoutesAndEndpoints evaluates GKEL4Routes in the Gateway's namespace and binds their endpoint selectors.
func (r *GatewayReconciler) reconcileRoutesAndEndpoints(ctx context.Context, gw *unstructured.Unstructured, gwIP, targetNetwork string) {
	routeList := &unstructured.UnstructuredList{}
	routeList.SetGroupVersionKind(GKEL4RouteGVK)

	if err := r.List(ctx, routeList, client.InNamespace(gw.GetNamespace())); err != nil {
		return
	}

	for _, route := range routeList.Items {
		rules, found, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
		if !found {
			continue
		}

		for _, rule := range rules {
			ruleMap, ok := rule.(map[string]interface{})
			if !ok {
				continue
			}

			backendRefs, found, _ := unstructured.NestedSlice(ruleMap, "backendRefs")
			if !found {
				continue
			}

			for _, bRef := range backendRefs {
				bMap, ok := bRef.(map[string]interface{})
				if !ok {
					continue
				}

				epSelectorName, _ := bMap["name"].(string)
				epPort := int32(80)
				if p, ok := bMap["port"].(int64); ok && p > 0 && p <= 65535 {
					epPort = int32(p)
				}

				if epSelectorName != "" {
					r.reconcileEndpointSliceForSelector(ctx, gw, &route, epSelectorName, epPort, gwIP, targetNetwork)
				}
			}
		}

		// Update GKEL4Route status conditions.
		now := metav1.Now().Rfc3339Copy().Format(time.RFC3339)
		routeStatus := map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{
					"type":               "Accepted",
					"status":             "True",
					"reason":             "Accepted",
					"message":            "GKEL4Route bound to Gateway",
					"lastTransitionTime": now,
				},
				map[string]interface{}{
					"type":               "Ready",
					"status":             "True",
					"reason":             "Ready",
					"message":            "Route programming active",
					"lastTransitionTime": now,
				},
			},
		}
		_ = unstructured.SetNestedField(route.Object, routeStatus, "status")
		if err := r.Status().Update(ctx, &route); err != nil {
			_ = r.Update(ctx, &route)
		}
	}
}

// reconcileEndpointSliceForSelector discovers backend Pod secondary IPs selected by GKEEndpointSelector
// and writes them into a dynamic headless Service and custom EndpointSlice.
func (r *GatewayReconciler) reconcileEndpointSliceForSelector(ctx context.Context, gw, route *unstructured.Unstructured, epSelectorName string, port int32, gwIP, targetNetwork string) {
	epSel := &unstructured.Unstructured{}
	epSel.SetGroupVersionKind(GKEEndpointSelectorGVK)
	if err := r.Get(ctx, client.ObjectKey{Namespace: gw.GetNamespace(), Name: epSelectorName}, epSel); err != nil {
		return
	}

	matchLabels, found, _ := unstructured.NestedStringMap(epSel.Object, "spec", "selector", "matchLabels")
	if !found || len(matchLabels) == 0 {
		return
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(gw.GetNamespace()), client.MatchingLabels(matchLabels)); err != nil {
		return
	}

	seenAddresses := make(map[string]bool)
	var backendAddresses []string

	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Primary source: parse JSON from networking.gke.io/pod-ips annotation.
		var foundPodIP bool
		if podIPsJSON, ok := pod.Annotations[AnnotationPodIPs]; ok && podIPsJSON != "" {
			var entries []PodIPEntry
			if err := json.Unmarshal([]byte(podIPsJSON), &entries); err == nil {
				for _, entry := range entries {
					if entry.NetworkName == targetNetwork && entry.IP != "" {
						foundPodIP = true
						if !seenAddresses[entry.IP] {
							seenAddresses[entry.IP] = true
							backendAddresses = append(backendAddresses, entry.IP)
						}
					}
				}
			}
		}

		// Fallback source: parse Multus CNI status from k8s.v1.cni.cncf.io/network-status annotation.
		if !foundPodIP {
			if netStatusJSON, ok := pod.Annotations[AnnotationNetworkStatus]; ok && netStatusJSON != "" {
				var statusList []map[string]interface{}
				if err := json.Unmarshal([]byte(netStatusJSON), &statusList); err == nil {
					for _, item := range statusList {
						if name, _ := item["name"].(string); strings.Contains(name, targetNetwork) {
							if ips, ok := item["ips"].([]interface{}); ok && len(ips) > 0 {
								if ipStr, ok := ips[0].(string); ok && ipStr != "" && !seenAddresses[ipStr] {
									seenAddresses[ipStr] = true
									backendAddresses = append(backendAddresses, ipStr)
								}
							}
						}
					}
				}
			}
		}
	}

	// Create or update dynamic headless Service.
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.GetName(),
			Namespace: gw.GetNamespace(),
			Labels: map[string]string{
				"networking.gke.io/gateway-name": gw.GetName(),
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:   corev1.ClusterIPNone,
			ExternalIPs: []string{gwIP},
			Ports: []corev1.ServicePort{
				{
					Name:       "service",
					Port:       port,
					TargetPort: intstr.FromInt(int(port)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	_ = r.applyService(ctx, svc)

	// Create or update dynamic EndpointSlice carrying Multus secondary IPs.
	protocolTCP := corev1.ProtocolTCP
	epPortName := "service"
	ready := true
	var endpoints []discoveryv1.Endpoint
	for _, addr := range backendAddresses {
		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses: []string{addr},
			Conditions: discoveryv1.EndpointConditions{
				Ready: &ready,
			},
		})
	}

	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-slice", gw.GetName()),
			Namespace: gw.GetNamespace(),
			Labels: map[string]string{
				discoveryv1.LabelServiceName: gw.GetName(),
				discoveryv1.LabelManagedBy:   "gem-nf-operator",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   endpoints,
		Ports: []discoveryv1.EndpointPort{
			{
				Name:     &epPortName,
				Port:     &port,
				Protocol: &protocolTCP,
			},
		},
	}
	_ = r.applyEndpointSlice(ctx, slice)
}

// reconcileCoreDNS ensures the Corefile in kube-system contains the rewrite rule for .gkegw.cluster.local.
func (r *GatewayReconciler) reconcileCoreDNS(ctx context.Context) {
	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "coredns-config"}, cm); err != nil {
		r.Log.Error(err, "Failed to get coredns-config from kube-system")
	} else if cm.Data != nil {
		corefile := cm.Data["Corefile"]
		if corefile != "" && !strings.Contains(corefile, ".gkegw.cluster.local") {
			lines := strings.Split(corefile, "\n")
			var newLines []string
			for _, line := range lines {
				newLines = append(newLines, line)
				if strings.Contains(line, "ready") {
					newLines = append(newLines, CoreDNSRewriteRule)
				}
			}
			cm.Data["Corefile"] = strings.Join(newLines, "\n")
			if err := r.Update(ctx, cm); err != nil {
				r.Log.Error(err, "Failed to update coredns-config with .gkegw.cluster.local rewrite rule")
			} else {
				r.Log.Info("Successfully updated coredns-config with .gkegw.cluster.local rewrite rule")
			}
		}
	}

	cmTmpl := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "coredns-template"}, cmTmpl); err == nil && cmTmpl.Data != nil {
		tmplData := cmTmpl.Data["coredns-template"]
		if tmplData != "" && !strings.Contains(tmplData, ".gkegw.cluster.local") {
			lines := strings.Split(tmplData, "\n")
			var newLines []string
			for _, line := range lines {
				newLines = append(newLines, line)
				if strings.Contains(line, "ready") {
					newLines = append(newLines, CoreDNSRewriteRule)
				}
			}
			cmTmpl.Data["coredns-template"] = strings.Join(newLines, "\n")
			_ = r.Update(ctx, cmTmpl)
		}
	}
}

// applyService creates or updates a Service resource.
func (r *GatewayReconciler) applyService(ctx context.Context, svc *corev1.Service) error {
	existing := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Namespace: svc.Namespace, Name: svc.Name}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return r.Create(ctx, svc)
		}
		return err
	}
	svc.ResourceVersion = existing.ResourceVersion
	return r.Update(ctx, svc)
}

// applyEndpointSlice creates or updates an EndpointSlice resource.
func (r *GatewayReconciler) applyEndpointSlice(ctx context.Context, slice *discoveryv1.EndpointSlice) error {
	existing := &discoveryv1.EndpointSlice{}
	err := r.Get(ctx, client.ObjectKey{Namespace: slice.Namespace, Name: slice.Name}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return r.Create(ctx, slice)
		}
		return err
	}
	slice.ResourceVersion = existing.ResourceVersion
	return r.Update(ctx, slice)
}

// cleanupDynamicResources removes dynamic Services and EndpointSlices associated with a Gateway.
func (r *GatewayReconciler) cleanupDynamicResources(ctx context.Context, gw *unstructured.Unstructured) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.GetName(),
			Namespace: gw.GetNamespace(),
		},
	}
	_ = r.Delete(ctx, svc)

	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-slice", gw.GetName()),
			Namespace: gw.GetNamespace(),
		},
	}
	_ = r.Delete(ctx, slice)
}

// findGatewaysForNamespaceObject maps an object change in a namespace to reconcile requests for all Gateways in that namespace.
func (r *GatewayReconciler) findGatewaysForNamespaceObject(ctx context.Context, obj client.Object) []ctrl.Request {
	gwList := &unstructured.UnstructuredList{}
	gwList.SetGroupVersionKind(GatewayGVK)
	if err := r.List(ctx, gwList, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var reqs []ctrl.Request
	for _, gw := range gwList.Items {
		reqs = append(reqs, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: gw.GetNamespace(),
				Name:      gw.GetName(),
			},
		})
	}
	return reqs
}

// SetupWithManager configures the Gateway controller and registers watches for related resources.
func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	gwObj := &unstructured.Unstructured{}
	gwObj.SetGroupVersionKind(GatewayGVK)

	routeObj := &unstructured.Unstructured{}
	routeObj.SetGroupVersionKind(GKEL4RouteGVK)

	epSelectorObj := &unstructured.Unstructured{}
	epSelectorObj.SetGroupVersionKind(GKEEndpointSelectorGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(gwObj).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.findGatewaysForNamespaceObject)).
		Watches(routeObj, handler.EnqueueRequestsFromMapFunc(r.findGatewaysForNamespaceObject)).
		Watches(epSelectorObj, handler.EnqueueRequestsFromMapFunc(r.findGatewaysForNamespaceObject)).
		Complete(r)
}
