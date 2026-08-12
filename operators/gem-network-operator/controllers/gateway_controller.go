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
	"k8s.io/client-go/util/retry"
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

	// Dynamically manage Services and EndpointSlices for attached GKEL4Routes and GKEEndpointSelectors.
	backendCount := r.reconcileRoutesAndEndpoints(ctx, gw, gwIP, targetNetwork)

	// Update Gateway status addresses and conditions to reflect Accepted and Programmed state.
	// The Programmed message carries the ready-backend count so an empty Gateway is
	// distinguishable from a healthy one when reading .status.
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
			"message":            fmt.Sprintf("Gateway programmed; %d ready backend endpoint(s)", backendCount),
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

	// Ensure CoreDNS rewrites *.gkegw.cluster.local to *.svc.cluster.local for Gateway service discovery.
	r.reconcileCoreDNS(ctx)

	// Watches on Pods, GKEL4Routes, and GKEEndpointSelectors drive event-based reconciliation;
	// this periodic requeue is only a drift-correction safety net.
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
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

// routeReferencesGateway reports whether the GKEL4Route's spec.parentRefs names the given Gateway.
// The GKEL4Route CRD's parentRefs entries carry an optional namespace; when set it must match too.
func routeReferencesGateway(route, gw *unstructured.Unstructured) bool {
	parentRefs, found, _ := unstructured.NestedSlice(route.Object, "spec", "parentRefs")
	if !found {
		return false
	}
	for _, ref := range parentRefs {
		refMap, ok := ref.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := refMap["name"].(string)
		if name != gw.GetName() {
			continue
		}
		if ns, ok := refMap["namespace"].(string); ok && ns != "" && ns != gw.GetNamespace() {
			continue
		}
		return true
	}
	return false
}

// reconcileRoutesAndEndpoints evaluates the GKEL4Routes bound to this Gateway via spec.parentRefs,
// aggregates their selected backend endpoints, and writes the Gateway's Service/EndpointSlice once.
// It returns the number of ready backend endpoints found.
func (r *GatewayReconciler) reconcileRoutesAndEndpoints(ctx context.Context, gw *unstructured.Unstructured, gwIP, targetNetwork string) int {
	routeList := &unstructured.UnstructuredList{}
	routeList.SetGroupVersionKind(GKEL4RouteGVK)

	if err := r.List(ctx, routeList, client.InNamespace(gw.GetNamespace())); err != nil {
		return 0
	}

	seenAddresses := make(map[string]bool)
	var backendAddresses []string
	seenPorts := make(map[int32]bool)
	var ports []int32
	haveBackendRefs := false

	for _, route := range routeList.Items {
		if !routeReferencesGateway(&route, gw) {
			continue
		}

		rules, found, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
		if found {
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
						haveBackendRefs = true
						backendAddresses = append(backendAddresses,
							r.collectEndpointsForSelector(ctx, gw, epSelectorName, targetNetwork, seenAddresses)...)
						if !seenPorts[epPort] {
							seenPorts[epPort] = true
							ports = append(ports, epPort)
						}
					}
				}
			}
		}

		// Update GKEL4Route status conditions (only on routes bound to this Gateway).
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

	if haveBackendRefs {
		r.writeServiceAndEndpointSlice(ctx, gw, gwIP, ports, backendAddresses)
	}
	return len(backendAddresses)
}

// podUsesSecondaryNetworks reports whether a pod carries any secondary-network annotation that
// makes it relevant to Gateway endpoint discovery.
func podUsesSecondaryNetworks(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}
	_, hasInterfaces := pod.Annotations["networking.gke.io/interfaces"]
	_, hasNetStatus := pod.Annotations[AnnotationNetworkStatus]
	return hasInterfaces || hasNetStatus
}

// networkStatusNameMatches compares a Multus network-status entry name (shaped
// "<namespace>/<net-attach-def-name>" or bare "<net-attach-def-name>") against the target network
// exactly. Substring matching is unsafe: network "vlan-1" must not match a pod on "vlan-12".
func networkStatusNameMatches(name, targetNetwork string) bool {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name == targetNetwork
}

// isPodReady reports whether the pod's PodReady condition is True. Pods that are Running but not
// ready (failing readiness probe, still starting) must not receive Gateway traffic, matching how
// the standard endpoint controllers treat Service backends.
func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// collectEndpointsForSelector discovers backend Pod secondary IPs selected by a GKEEndpointSelector.
// seenAddresses is shared across selectors so the same IP isn't added twice for one Gateway.
func (r *GatewayReconciler) collectEndpointsForSelector(ctx context.Context, gw *unstructured.Unstructured, epSelectorName, targetNetwork string, seenAddresses map[string]bool) []string {
	epSel := &unstructured.Unstructured{}
	epSel.SetGroupVersionKind(GKEEndpointSelectorGVK)
	if err := r.Get(ctx, client.ObjectKey{Namespace: gw.GetNamespace(), Name: epSelectorName}, epSel); err != nil {
		return nil
	}

	matchLabels, found, _ := unstructured.NestedStringMap(epSel.Object, "spec", "selector", "matchLabels")
	if !found || len(matchLabels) == 0 {
		return nil
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace(gw.GetNamespace()), client.MatchingLabels(matchLabels)); err != nil {
		return nil
	}

	var backendAddresses []string

	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodRunning || !isPodReady(&pod) {
			continue
		}

		// Parse Multus CNI status from the k8s.v1.cni.cncf.io/network-status annotation — the one
		// source of secondary-IP truth in GEM (nothing writes networking.gke.io/pod-ips here).
		if netStatusJSON, ok := pod.Annotations[AnnotationNetworkStatus]; ok && netStatusJSON != "" {
			var statusList []map[string]interface{}
			if err := json.Unmarshal([]byte(netStatusJSON), &statusList); err == nil {
				for _, item := range statusList {
					name, _ := item["name"].(string)
					if networkStatusNameMatches(name, targetNetwork) {
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

	return backendAddresses
}

// writeServiceAndEndpointSlice creates or updates the Gateway's dynamic headless Service and the
// EndpointSlice carrying the aggregated Multus secondary IPs of all its routes' backends.
func (r *GatewayReconciler) writeServiceAndEndpointSlice(ctx context.Context, gw *unstructured.Unstructured, gwIP string, ports []int32, backendAddresses []string) {
	portName := func(p int32) string {
		if len(ports) == 1 {
			return "service"
		}
		return fmt.Sprintf("service-%d", p)
	}

	var svcPorts []corev1.ServicePort
	for _, p := range ports {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:       portName(p),
			Port:       p,
			TargetPort: intstr.FromInt(int(p)),
			Protocol:   corev1.ProtocolTCP,
		})
	}

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
			Ports:       svcPorts,
		},
	}
	if err := r.applyService(ctx, svc); err != nil {
		r.Log.Error(err, "Failed to apply dynamic Service for Gateway", "gateway", gw.GetName(), "namespace", gw.GetNamespace())
	}

	protocolTCP := corev1.ProtocolTCP
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

	var slicePorts []discoveryv1.EndpointPort
	for i := range ports {
		name := portName(ports[i])
		slicePorts = append(slicePorts, discoveryv1.EndpointPort{
			Name:     &name,
			Port:     &ports[i],
			Protocol: &protocolTCP,
		})
	}

	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-slice", gw.GetName()),
			Namespace: gw.GetNamespace(),
			Labels: map[string]string{
				discoveryv1.LabelServiceName: gw.GetName(),
				discoveryv1.LabelManagedBy:   "gem-network-operator",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   endpoints,
		Ports:       slicePorts,
	}
	if err := r.applyEndpointSlice(ctx, slice); err != nil {
		r.Log.Error(err, "Failed to apply dynamic EndpointSlice for Gateway", "gateway", gw.GetName(), "namespace", gw.GetNamespace())
	}
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
				// No reload plugin is configured in GEM's Corefile, so CoreDNS keeps serving its
				// in-memory config until restarted. Bounce it only when the ConfigMap actually changed.
				r.restartCoreDNS(ctx)
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

// restartCoreDNS deletes the kube-dns pods so their Deployment recreates them with the updated
// Corefile, mirroring the equivalent Ansible task's `kubectl delete pod -l k8s-app=kube-dns`.
func (r *GatewayReconciler) restartCoreDNS(ctx context.Context) {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.InNamespace("kube-system"), client.MatchingLabels{"k8s-app": "kube-dns"}); err != nil {
		r.Log.Error(err, "Failed to list CoreDNS pods for restart")
		return
	}
	for i := range podList.Items {
		if err := r.Delete(ctx, &podList.Items[i]); err != nil {
			r.Log.Error(err, "Failed to delete CoreDNS pod for Corefile reload", "pod", podList.Items[i].Name)
		}
	}
}

// applyService creates or updates a Service resource, retrying on optimistic-concurrency conflicts.
func (r *GatewayReconciler) applyService(ctx context.Context, svc *corev1.Service) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
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
	})
}

// applyEndpointSlice creates or updates an EndpointSlice resource, retrying on optimistic-concurrency conflicts.
func (r *GatewayReconciler) applyEndpointSlice(ctx context.Context, slice *discoveryv1.EndpointSlice) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
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
	})
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

// findGatewaysForNamespaceObject maps an object change in a namespace to reconcile requests for all
// Gateways in that namespace. Pod events are filtered to pods that actually participate in
// secondary networking — without this, every pod create/update/delete anywhere in the cluster
// would fan out into reconciles of all Gateways in its namespace.
func (r *GatewayReconciler) findGatewaysForNamespaceObject(ctx context.Context, obj client.Object) []ctrl.Request {
	if pod, ok := obj.(*corev1.Pod); ok && !podUsesSecondaryNetworks(pod) {
		return nil
	}

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
