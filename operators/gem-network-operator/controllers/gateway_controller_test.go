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
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
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

	// Backend Pod 1 with networking.gke.io/pod-ips
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-pod-1",
			Namespace: "prod",
			Labels: map[string]string{
				"app": "web",
			},
			Annotations: map[string]string{
				AnnotationPodIPs: `[{"networkName":"vlan-123","ip":"172.16.12.10"}]`,
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
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

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "prod",
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
