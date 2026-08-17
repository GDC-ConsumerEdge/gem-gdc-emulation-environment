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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func setupGatewayTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = discoveryv1.AddToScheme(s)
	return s
}

func TestGatewayReconciler_Reconcile_StaticAddress(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "static-gw",
				"namespace": "default",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-123",
				},
			},
			"spec": map[string]interface{}{
				"gatewayClassName": "gke-l4-regional-internal-gateway",
				"addresses": []interface{}{
					map[string]interface{}{
						"type":  "IPAddress",
						"value": "172.16.12.99",
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "static-gw"}}
	res, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// The controller watches Pods, GKEL4Routes, and GKEEndpointSelectors, so the periodic requeue
	// is only a drift-correction safety net — it must not be an aggressive 2s poll.
	if res.RequeueAfter != 30*time.Second {
		t.Errorf("Expected steady-state RequeueAfter 30s, got %v", res.RequeueAfter)
	}

	updatedGW := &unstructured.Unstructured{}
	updatedGW.SetGroupVersionKind(GatewayGVK)
	err = fakeClient.Get(ctx, req.NamespacedName, updatedGW)
	if err != nil {
		t.Fatalf("Failed to get updated Gateway: %v", err)
	}

	// Verify status.addresses[0].value == 172.16.12.99
	addrs, found, _ := unstructured.NestedSlice(updatedGW.Object, "status", "addresses")
	if !found || len(addrs) == 0 {
		t.Fatalf("Expected status.addresses on Gateway")
	}
	addrMap := addrs[0].(map[string]interface{})
	if addrMap["value"] != "172.16.12.99" {
		t.Errorf("Expected status.addresses[0].value == 172.16.12.99, got %v", addrMap["value"])
	}
}

func TestGatewayReconciler_Reconcile_DynamicAllocationFromGKEGatewayCIDR(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "dyn-gw",
				"namespace": "default",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-123",
				},
			},
			"spec": map[string]interface{}{
				"gatewayClassName": "gke-l4-regional-internal-gateway",
			},
		},
	}

	gwCIDR := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEGatewayCIDR",
			"metadata": map[string]interface{}{
				"name": "vlan-123-gw-cidr",
			},
			"spec": map[string]interface{}{
				"network": "vlan-123",
				"ip4cidr": "172.16.12.224/28",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, gwCIDR).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "dyn-gw"}}
	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	updatedGW := &unstructured.Unstructured{}
	updatedGW.SetGroupVersionKind(GatewayGVK)
	_ = fakeClient.Get(ctx, req.NamespacedName, updatedGW)

	addrs, found, _ := unstructured.NestedSlice(updatedGW.Object, "status", "addresses")
	if !found || len(addrs) == 0 {
		t.Fatalf("Expected status.addresses on Gateway")
	}
	addrMap := addrs[0].(map[string]interface{})
	// First usable host IP in 172.16.12.224/28 is 172.16.12.225
	if addrMap["value"] != "172.16.12.225" {
		t.Errorf("Expected allocated VIP 172.16.12.225, got %v", addrMap["value"])
	}
}

// TestGatewayReconciler_Reconcile_TwoGatewaysSameCIDRCollide documents a KNOWN LIMITATION rather
// than desired behavior: VIP "allocation" always returns the CIDR's first host address with no
// tracking of issued IPs, so two Gateways referencing the same GKEGatewayCIDR receive the SAME VIP
// and will collide. Real IPAM is future work (see AGENTS.md); if allocation tracking is ever
// implemented, replace this test with one asserting the two Gateways get different VIPs.
func TestGatewayReconciler_Reconcile_TwoGatewaysSameCIDRCollide(t *testing.T) {
	scheme := setupGatewayTestScheme()

	buildGW := func(name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "gateway.networking.k8s.io/v1",
				"kind":       "Gateway",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": "default",
					"annotations": map[string]interface{}{
						AnnotationNetwork: "vlan-123",
					},
				},
				"spec": map[string]interface{}{
					"gatewayClassName": "gke-cluster-ip",
				},
			},
		}
	}

	gwCIDR := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEGatewayCIDR",
			"metadata":   map[string]interface{}{"name": "vlan-123-cidr"},
			"spec": map[string]interface{}{
				"network": "vlan-123",
				"ip4cidr": "172.16.12.224/28",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(buildGW("gw-one"), buildGW("gw-two"), gwCIDR).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	getVIP := func(t *testing.T, name string) string {
		t.Helper()
		req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: name}}
		if _, err := reconciler.Reconcile(ctx, req); err != nil {
			t.Fatalf("Reconcile of %s returned error: %v", name, err)
		}
		gw := &unstructured.Unstructured{}
		gw.SetGroupVersionKind(GatewayGVK)
		if err := fakeClient.Get(ctx, req.NamespacedName, gw); err != nil {
			t.Fatalf("Failed to get Gateway %s: %v", name, err)
		}
		addrs, found, _ := unstructured.NestedSlice(gw.Object, "status", "addresses")
		if !found || len(addrs) == 0 {
			t.Fatalf("Expected status.addresses on Gateway %s", name)
		}
		vip, _ := addrs[0].(map[string]interface{})["value"].(string)
		return vip
	}

	vipOne := getVIP(t, "gw-one")
	vipTwo := getVIP(t, "gw-two")

	// Both Gateways get the first host address of the CIDR — the documented collision.
	if vipOne != "172.16.12.225" || vipTwo != "172.16.12.225" {
		t.Errorf("Known-limitation contract changed: expected both Gateways to receive 172.16.12.225 "+
			"(first host address, no allocation tracking), got %q and %q — if real IPAM was implemented, "+
			"update this test and AGENTS.md's Future Work section", vipOne, vipTwo)
	}
}

func TestGatewayReconciler_Reconcile_RoutesAndEndpoints(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "my-gateway",
				"namespace": "prod",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-123",
				},
			},
			"spec": map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{
						"type":  "IPAddress",
						"value": "172.16.12.200",
					},
				},
			},
		},
	}

	epSelector := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEEndpointSelector",
			"metadata": map[string]interface{}{
				"name":      "web-selector",
				"namespace": "prod",
			},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "web",
					},
				},
			},
		},
	}

	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEL4Route",
			"metadata": map[string]interface{}{
				"name":      "web-route",
				"namespace": "prod",
			},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{
						"name": "my-gateway",
					},
				},
				"rules": []interface{}{
					map[string]interface{}{
						"backendRefs": []interface{}{
							map[string]interface{}{
								"name": "web-selector",
								"port": int64(8080),
							},
						},
					},
				},
			},
		},
	}

	// Backend Pod 1 with Multus network-status
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-pod-1",
			Namespace: "prod",
			Labels: map[string]string{
				"app": "web",
			},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-123","ips":["172.16.12.10"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	// Backend Pod 2 with Multus fallback status
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-pod-2",
			Namespace: "prod",
			Labels: map[string]string{
				"app": "web",
			},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-123","ips":["172.16.12.20"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, epSelector, route, pod1, pod2).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "prod", Name: "my-gateway"}}
	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// 1. Verify dynamic Service created in 'prod' namespace
	svc := &corev1.Service{}
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "my-gateway"}, svc)
	if err != nil {
		t.Fatalf("Expected dynamic Service 'my-gateway' in prod namespace: %v", err)
	}
	if svc.Spec.ClusterIP != corev1.ClusterIPNone {
		t.Errorf("Expected headless service ClusterIPNone, got %s", svc.Spec.ClusterIP)
	}
	if len(svc.Spec.ExternalIPs) == 0 || svc.Spec.ExternalIPs[0] != "172.16.12.200" {
		t.Errorf("Expected ExternalIPs [172.16.12.200], got %v", svc.Spec.ExternalIPs)
	}
	if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].Port != 8080 {
		t.Errorf("Expected port 8080 on Service, got %v", svc.Spec.Ports)
	}

	// 2. Verify dynamic EndpointSlice created in 'prod' namespace
	slice := &discoveryv1.EndpointSlice{}
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "my-gateway-slice"}, slice)
	if err != nil {
		t.Fatalf("Expected dynamic EndpointSlice 'my-gateway-slice': %v", err)
	}
	if len(slice.Endpoints) != 2 {
		t.Fatalf("Expected 2 backend endpoints in EndpointSlice, got %d", len(slice.Endpoints))
	}
	epIPs := map[string]bool{}
	for _, ep := range slice.Endpoints {
		if len(ep.Addresses) > 0 {
			epIPs[ep.Addresses[0]] = true
		}
		if ep.Conditions.Ready == nil || !*ep.Conditions.Ready {
			t.Errorf("Expected endpoint condition Ready=true")
		}
	}
	if !epIPs["172.16.12.10"] || !epIPs["172.16.12.20"] {
		t.Errorf("Expected endpoints 172.16.12.10 and 172.16.12.20, got %v", epIPs)
	}

	// 3. Verify GKEL4Route status conditions
	updatedRoute := &unstructured.Unstructured{}
	updatedRoute.SetGroupVersionKind(GKEL4RouteGVK)
	_ = fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "web-route"}, updatedRoute)
	conds, found, _ := unstructured.NestedSlice(updatedRoute.Object, "status", "conditions")
	if !found || len(conds) < 2 {
		t.Errorf("Expected GKEL4Route status conditions, got %v", conds)
	}
}

func TestGatewayReconciler_Reconcile_CoreDNSInjections(t *testing.T) {
	scheme := setupGatewayTestScheme()

	cmConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns-config",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"Corefile": `.:53 {
    errors
    health
    ready
    kubernetes cluster.local in-addr.arpa ip6.arpa {
        pods insecure
        fallthrough in-addr.arpa ip6.arpa
    }
}`,
		},
	}

	cmTemplate := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns-template",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"coredns-template": `.:53 {
    errors
    ready
    kubernetes cluster.local
}`,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cmConfig, cmTemplate).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	reconciler.reconcileCoreDNS(ctx)

	// Verify coredns-config Corefile has been injected with rewrite rule
	updatedCM := &corev1.ConfigMap{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "coredns-config"}, updatedCM)
	if !strings.Contains(updatedCM.Data["Corefile"], ".gkegw.cluster.local .svc.cluster.local") {
		t.Errorf("Expected .gkegw.cluster.local rewrite rule in coredns-config, got:\n%s", updatedCM.Data["Corefile"])
	}

	// Verify coredns-template has been injected
	updatedTmpl := &corev1.ConfigMap{}
	_ = fakeClient.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "coredns-template"}, updatedTmpl)
	if !strings.Contains(updatedTmpl.Data["coredns-template"], ".gkegw.cluster.local .svc.cluster.local") {
		t.Errorf("Expected .gkegw.cluster.local rewrite rule in coredns-template, got:\n%s", updatedTmpl.Data["coredns-template"])
	}
}

func TestGatewayReconciler_Reconcile_CoreDNSTriggersReload(t *testing.T) {
	scheme := setupGatewayTestScheme()

	cmConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns-config",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"Corefile": `.:53 {
    errors
    ready
    kubernetes cluster.local
}`,
		},
	}

	corednsPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns-abc123",
			Namespace: "kube-system",
			Labels:    map[string]string{"k8s-app": "kube-dns"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cmConfig, corednsPod).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	reconciler.reconcileCoreDNS(ctx)

	// The Corefile was changed, so CoreDNS pods must be restarted for it to take effect:
	// no reload plugin is configured anywhere in GEM, so a stale in-memory Corefile
	// would otherwise persist indefinitely.
	err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "coredns-abc123"}, &corev1.Pod{})
	if err == nil {
		t.Errorf("Expected kube-dns pod to be deleted after Corefile change (reload trigger), but it still exists")
	}
}

func TestGatewayReconciler_Reconcile_CoreDNSNoReloadWhenAlreadyIdempotent(t *testing.T) {
	scheme := setupGatewayTestScheme()

	cmConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns-config",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"Corefile": `.:53 {
    errors
    ready
    rewrite name suffix .gkegw.cluster.local .svc.cluster.local
    kubernetes cluster.local
}`,
		},
	}

	corednsPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns-abc123",
			Namespace: "kube-system",
			Labels:    map[string]string{"k8s-app": "kube-dns"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cmConfig, corednsPod).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	reconciler.reconcileCoreDNS(ctx)

	// The rewrite rule was already present: no ConfigMap change, so CoreDNS must NOT be bounced.
	err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "coredns-abc123"}, &corev1.Pod{})
	if err != nil {
		t.Errorf("Expected kube-dns pod to survive an idempotent no-op reconcile, got: %v", err)
	}
}

func TestGatewayReconciler_Reconcile_DeletionCleanup(t *testing.T) {
	scheme := setupGatewayTestScheme()

	now := metav1.Now()
	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":              "dying-gw",
				"namespace":         "default",
				"deletionTimestamp": now.Rfc3339Copy().Format(time.RFC3339),
				"finalizers":        []interface{}{GatewayFinalizer},
			},
		},
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dying-gw",
			Namespace: "default",
		},
	}
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dying-gw-slice",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, svc, slice).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "dying-gw"}}
	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile error during deletion: %v", err)
	}

	// Verify dynamic Service is deleted
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "dying-gw"}, &corev1.Service{})
	if err == nil {
		t.Errorf("Expected dynamic Service to be deleted")
	}

	// Verify dynamic EndpointSlice is deleted
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "dying-gw-slice"}, &discoveryv1.EndpointSlice{})
	if err == nil {
		t.Errorf("Expected dynamic EndpointSlice to be deleted")
	}
}

func TestGatewayReconciler_Reconcile_MissingNetworkAnnotation(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "no-annot-gw",
				"namespace": "default",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "no-annot-gw"}}
	res, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Expected no error when annotation missing, got: %v", err)
	}
	if res.RequeueAfter != 5*time.Second {
		t.Errorf("Expected RequeueAfter 5s, got: %v", res.RequeueAfter)
	}
}

func TestGatewayReconciler_Reconcile_MissingGatewayCIDR(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "waiting-gw",
				"namespace": "default",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "nonexistent-net",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "waiting-gw"}}
	res, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Expected no error when CIDR missing, got: %v", err)
	}
	if res.RequeueAfter != 5*time.Second {
		t.Errorf("Expected RequeueAfter 5s, got: %v", res.RequeueAfter)
	}
}

func TestGatewayReconciler_Reconcile_IgnoresRouteForOtherGateway(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gwA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "gw-a",
				"namespace": "prod",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-a",
				},
			},
			"spec": map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "IPAddress", "value": "10.0.0.1"},
				},
			},
		},
	}
	gwB := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "gw-b",
				"namespace": "prod",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-b",
				},
			},
			"spec": map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "IPAddress", "value": "10.0.0.2"},
				},
			},
		},
	}

	selA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEEndpointSelector",
			"metadata":   map[string]interface{}{"name": "sel-a", "namespace": "prod"},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"app": "svc-a"},
				},
			},
		},
	}
	selB := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEEndpointSelector",
			"metadata":   map[string]interface{}{"name": "sel-b", "namespace": "prod"},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"app": "svc-b"},
				},
			},
		},
	}

	// a-route sorts before b-route in list order, so the buggy unfiltered loop
	// processes b-route last and overwrites gw-a's Service/EndpointSlice.
	routeA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEL4Route",
			"metadata":   map[string]interface{}{"name": "a-route", "namespace": "prod"},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{"name": "gw-a"},
				},
				"rules": []interface{}{
					map[string]interface{}{
						"backendRefs": []interface{}{
							map[string]interface{}{"name": "sel-a", "port": int64(8080)},
						},
					},
				},
			},
		},
	}
	routeB := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEL4Route",
			"metadata":   map[string]interface{}{"name": "b-route", "namespace": "prod"},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{"name": "gw-b"},
				},
				"rules": []interface{}{
					map[string]interface{}{
						"backendRefs": []interface{}{
							map[string]interface{}{"name": "sel-b", "port": int64(9090)},
						},
					},
				},
			},
		},
	}

	podA := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-a",
			Namespace: "prod",
			Labels:    map[string]string{"app": "svc-a"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-a","ips":["172.16.1.10"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	podB := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-b",
			Namespace: "prod",
			Labels:    map[string]string{"app": "svc-b"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-b","ips":["172.16.2.10"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gwA, gwB, selA, selB, routeA, routeB, podA, podB).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "prod", Name: "gw-a"}}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// gw-a's EndpointSlice must contain only gw-a's route's backend IP.
	slice := &discoveryv1.EndpointSlice{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "gw-a-slice"}, slice); err != nil {
		t.Fatalf("Expected EndpointSlice gw-a-slice: %v", err)
	}
	if len(slice.Endpoints) != 1 || len(slice.Endpoints[0].Addresses) != 1 || slice.Endpoints[0].Addresses[0] != "172.16.1.10" {
		t.Errorf("Expected exactly gw-a's backend 172.16.1.10 in EndpointSlice, got %v", slice.Endpoints)
	}

	// gw-a's Service must carry a-route's port, not b-route's.
	svc := &corev1.Service{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "gw-a"}, svc); err != nil {
		t.Fatalf("Expected Service gw-a: %v", err)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 8080 {
		t.Errorf("Expected only port 8080 on gw-a Service, got %v", svc.Spec.Ports)
	}

	// b-route belongs to gw-b and must not have been stamped by gw-a's reconcile.
	updatedRouteB := &unstructured.Unstructured{}
	updatedRouteB.SetGroupVersionKind(GKEL4RouteGVK)
	_ = fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "b-route"}, updatedRouteB)
	if conds, found, _ := unstructured.NestedSlice(updatedRouteB.Object, "status", "conditions"); found && len(conds) > 0 {
		t.Errorf("Expected no status conditions on b-route after reconciling gw-a, got %v", conds)
	}
}

func TestGatewayReconciler_Reconcile_MultipleBackendRefsMerge(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "merge-gw",
				"namespace": "prod",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-123",
				},
			},
			"spec": map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "IPAddress", "value": "172.16.12.200"},
				},
			},
		},
	}

	sel1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEEndpointSelector",
			"metadata":   map[string]interface{}{"name": "sel-1", "namespace": "prod"},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"app": "w1"},
				},
			},
		},
	}
	sel2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEEndpointSelector",
			"metadata":   map[string]interface{}{"name": "sel-2", "namespace": "prod"},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"app": "w2"},
				},
			},
		},
	}

	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEL4Route",
			"metadata":   map[string]interface{}{"name": "merge-route", "namespace": "prod"},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{"name": "merge-gw"},
				},
				"rules": []interface{}{
					map[string]interface{}{
						"backendRefs": []interface{}{
							map[string]interface{}{"name": "sel-1", "port": int64(8080)},
							map[string]interface{}{"name": "sel-2", "port": int64(8080)},
						},
					},
				},
			},
		},
	}

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "w1-pod",
			Namespace: "prod",
			Labels:    map[string]string{"app": "w1"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-123","ips":["172.16.12.10"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "w2-pod",
			Namespace: "prod",
			Labels:    map[string]string{"app": "w2"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-123","ips":["172.16.12.20"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, sel1, sel2, route, pod1, pod2).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "prod", Name: "merge-gw"}}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	slice := &discoveryv1.EndpointSlice{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "merge-gw-slice"}, slice); err != nil {
		t.Fatalf("Expected EndpointSlice merge-gw-slice: %v", err)
	}
	epIPs := map[string]bool{}
	for _, ep := range slice.Endpoints {
		for _, addr := range ep.Addresses {
			epIPs[addr] = true
		}
	}
	if !epIPs["172.16.12.10"] || !epIPs["172.16.12.20"] {
		t.Errorf("Expected union of both backendRefs' endpoints (172.16.12.10, 172.16.12.20), got %v", epIPs)
	}
}

func TestGatewayReconciler_Reconcile_ServiceApplyErrorLogged(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "err-gw",
				"namespace": "prod",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-123",
				},
			},
			"spec": map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "IPAddress", "value": "172.16.12.200"},
				},
			},
		},
	}

	sel := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEEndpointSelector",
			"metadata":   map[string]interface{}{"name": "err-sel", "namespace": "prod"},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"app": "err-app"},
				},
			},
		},
	}

	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEL4Route",
			"metadata":   map[string]interface{}{"name": "err-route", "namespace": "prod"},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{"name": "err-gw"},
				},
				"rules": []interface{}{
					map[string]interface{}{
						"backendRefs": []interface{}{
							map[string]interface{}{"name": "err-sel", "port": int64(8080)},
						},
					},
				},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "err-pod",
			Namespace: "prod",
			Labels:    map[string]string{"app": "err-app"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-123","ips":["172.16.12.10"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	failServiceWrites := interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*corev1.Service); ok {
				return errors.New("simulated service apply failure")
			}
			return c.Create(ctx, obj, opts...)
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, sel, route, pod).
		WithInterceptorFuncs(failServiceWrites).
		Build()

	var logged []string
	captureLog := funcr.New(func(prefix, args string) {
		logged = append(logged, prefix+" "+args)
	}, funcr.Options{})

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    captureLog,
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "prod", Name: "err-gw"}}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var foundErrLog bool
	for _, entry := range logged {
		if strings.Contains(entry, "simulated service apply failure") {
			foundErrLog = true
		}
	}
	if !foundErrLog {
		t.Errorf("Expected the Service apply failure to be logged, captured logs: %v", logged)
	}
}

func TestGatewayReconciler_Reconcile_ExcludesNotReadyPods(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "ready-gw",
				"namespace": "prod",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-123",
				},
			},
			"spec": map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "IPAddress", "value": "172.16.12.200"},
				},
			},
		},
	}

	sel := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEEndpointSelector",
			"metadata":   map[string]interface{}{"name": "ready-sel", "namespace": "prod"},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"app": "ready-app"},
				},
			},
		},
	}

	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEL4Route",
			"metadata":   map[string]interface{}{"name": "ready-route", "namespace": "prod"},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{"name": "ready-gw"},
				},
				"rules": []interface{}{
					map[string]interface{}{
						"backendRefs": []interface{}{
							map[string]interface{}{"name": "ready-sel", "port": int64(8080)},
						},
					},
				},
			},
		},
	}

	readyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ready-pod",
			Namespace: "prod",
			Labels:    map[string]string{"app": "ready-app"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-123","ips":["172.16.12.10"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	// Running but failing its readiness probe.
	notReadyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "not-ready-pod",
			Namespace: "prod",
			Labels:    map[string]string{"app": "ready-app"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-123","ips":["172.16.12.11"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
		},
	}
	// Running with no Ready condition at all (e.g. very early in startup).
	noCondPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-cond-pod",
			Namespace: "prod",
			Labels:    map[string]string{"app": "ready-app"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-123","ips":["172.16.12.12"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, sel, route, readyPod, notReadyPod, noCondPod).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "prod", Name: "ready-gw"}}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	slice := &discoveryv1.EndpointSlice{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "ready-gw-slice"}, slice); err != nil {
		t.Fatalf("Expected EndpointSlice ready-gw-slice: %v", err)
	}
	if len(slice.Endpoints) != 1 || len(slice.Endpoints[0].Addresses) != 1 || slice.Endpoints[0].Addresses[0] != "172.16.12.10" {
		t.Errorf("Expected only the ready pod's IP 172.16.12.10 in EndpointSlice, got %v", slice.Endpoints)
	}
}

func TestGatewayReconciler_Reconcile_NetworkStatusExactMatchNotSubstring(t *testing.T) {
	scheme := setupGatewayTestScheme()

	// Gateway targets network "vlan-1"; a pod attached to "vlan-12" must not match.
	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "exact-gw",
				"namespace": "prod",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-1",
				},
			},
			"spec": map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "IPAddress", "value": "172.16.12.200"},
				},
			},
		},
	}

	sel := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEEndpointSelector",
			"metadata":   map[string]interface{}{"name": "exact-sel", "namespace": "prod"},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"app": "exact-app"},
				},
			},
		},
	}

	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEL4Route",
			"metadata":   map[string]interface{}{"name": "exact-route", "namespace": "prod"},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{"name": "exact-gw"},
				},
				"rules": []interface{}{
					map[string]interface{}{
						"backendRefs": []interface{}{
							map[string]interface{}{"name": "exact-sel", "port": int64(8080)},
						},
					},
				},
			},
		},
	}

	// Pod on vlan-12 (name contains "vlan-1" as a substring) — must be excluded.
	wrongNetPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wrong-net-pod",
			Namespace: "prod",
			Labels:    map[string]string{"app": "exact-app"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-12","ips":["172.16.99.10"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	// Pod genuinely on vlan-1 — must be included.
	rightNetPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "right-net-pod",
			Namespace: "prod",
			Labels:    map[string]string{"app": "exact-app"},
			Annotations: map[string]string{
				AnnotationNetworkStatus: `[{"name":"prod/vlan-1","ips":["172.16.1.10"]}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, sel, route, wrongNetPod, rightNetPod).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "prod", Name: "exact-gw"}}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	slice := &discoveryv1.EndpointSlice{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "exact-gw-slice"}, slice); err != nil {
		t.Fatalf("Expected EndpointSlice exact-gw-slice: %v", err)
	}
	if len(slice.Endpoints) != 1 || len(slice.Endpoints[0].Addresses) != 1 || slice.Endpoints[0].Addresses[0] != "172.16.1.10" {
		t.Errorf("Expected only vlan-1's pod IP 172.16.1.10 (vlan-12 must not substring-match), got %v", slice.Endpoints)
	}
}

func TestGatewayReconciler_Reconcile_NoBackendsReflectedInStatus(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "empty-gw",
				"namespace": "prod",
				"annotations": map[string]interface{}{
					AnnotationNetwork: "vlan-123",
				},
			},
			"spec": map[string]interface{}{
				"addresses": []interface{}{
					map[string]interface{}{"type": "IPAddress", "value": "172.16.12.200"},
				},
			},
		},
	}

	sel := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEEndpointSelector",
			"metadata":   map[string]interface{}{"name": "empty-sel", "namespace": "prod"},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"app": "nothing-matches-this"},
				},
			},
		},
	}

	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEL4Route",
			"metadata":   map[string]interface{}{"name": "empty-route", "namespace": "prod"},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{"name": "empty-gw"},
				},
				"rules": []interface{}{
					map[string]interface{}{
						"backendRefs": []interface{}{
							map[string]interface{}{"name": "empty-sel", "port": int64(8080)},
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, sel, route).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "prod", Name: "empty-gw"}}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedGW := &unstructured.Unstructured{}
	updatedGW.SetGroupVersionKind(GatewayGVK)
	if err := fakeClient.Get(ctx, req.NamespacedName, updatedGW); err != nil {
		t.Fatalf("Failed to get Gateway: %v", err)
	}

	conditions, _, _ := unstructured.NestedSlice(updatedGW.Object, "status", "conditions")
	var programmedCond map[string]interface{}
	for _, c := range conditions {
		if cMap, ok := c.(map[string]interface{}); ok && cMap["type"] == "Programmed" {
			programmedCond = cMap
		}
	}
	if programmedCond == nil {
		t.Fatalf("Expected a Programmed condition, got %v", conditions)
	}
	// A Gateway with zero ready backends must be distinguishable from a healthy one:
	// the Programmed message reports the ready-backend count.
	msg, _ := programmedCond["message"].(string)
	if !strings.Contains(msg, "0 ready backend") {
		t.Errorf("Expected Programmed message to reflect the zero-backend state, got %q", msg)
	}
}

func TestGatewayReconciler_findGatewaysForNamespaceObject(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "gw-1",
				"namespace": "prod",
			},
		},
	}
	gw2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "gw-2",
				"namespace": "staging",
			},
		},
	}

	// A pod attached to a secondary network (networking.gke.io/interfaces) is relevant.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "prod",
			Annotations: map[string]string{
				"networking.gke.io/interfaces": `[{"interfaceName":"eth1","network":"vlan-123"}]`,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw1, gw2, pod).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	requests := reconciler.findGatewaysForNamespaceObject(ctx, pod)
	if len(requests) != 1 {
		t.Fatalf("Expected 1 request for 'prod' namespace, got %d", len(requests))
	}
	if requests[0].Name != "gw-1" || requests[0].Namespace != "prod" {
		t.Errorf("Expected request for prod/gw-1, got %v", requests[0])
	}
}

func TestGatewayReconciler_findGatewaysForNamespaceObject_IgnoresUnrelatedPod(t *testing.T) {
	scheme := setupGatewayTestScheme()

	gw := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "gw-1",
				"namespace": "prod",
			},
		},
	}

	// A pod with no secondary-network annotations must not trigger Gateway reconciles:
	// unrelated cluster pod churn would otherwise fan out into reconcile storms.
	unrelatedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unrelated-pod",
			Namespace: "prod",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, unrelatedPod).
		Build()

	reconciler := &GatewayReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	requests := reconciler.findGatewaysForNamespaceObject(ctx, unrelatedPod)
	if len(requests) != 0 {
		t.Errorf("Expected no requests for a pod without secondary-network annotations, got %d: %v", len(requests), requests)
	}

	// Non-Pod objects (GKEL4Route, GKEEndpointSelector) still map through unfiltered.
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(GKEL4RouteGVK)
	route.SetName("some-route")
	route.SetNamespace("prod")
	requests = reconciler.findGatewaysForNamespaceObject(ctx, route)
	if len(requests) != 1 {
		t.Errorf("Expected 1 request for a route object in prod, got %d", len(requests))
	}
}
