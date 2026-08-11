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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupNetworkTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return s
}

func TestNetworkReconciler_Reconcile_CompleteNetwork(t *testing.T) {
	scheme := setupNetworkTestScheme()

	// Create test namespaces: default, prod, kube-system
	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	nsProd := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod"}}
	nsKubeSystem := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}

	// Define a complete Network custom resource with all GDCE annotations
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
				"annotations": map[string]interface{}{
					AnnotationVLANID:          "123",
					AnnotationVLANMTU:         "1410",
					AnnotationLBServiceVIPs:   `["172.16.12.200", "172.16.12.201"]`,
					AnnotationGatewayPodCIDR:  "10.12.0.0/22",
					AnnotationPerNodeIPAMSize: "24",
				},
			},
			"spec": map[string]interface{}{
				"type":     "L2",
				"gateway4": "172.16.12.1",
				"l2NetworkConfig": map[string]interface{}{
					"prefixLength4": int64(24),
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, nsProd, nsKubeSystem, netObj).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "vlan-123"},
	}

	ctx := context.Background()
	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}

	if result.RequeueAfter != 30*time.Second {
		t.Errorf("Expected RequeueAfter 30s, got %v", result.RequeueAfter)
	}

	// 1. Verify NetworkAttachmentDefinition was dynamically generated in both user namespaces
	for _, nsName := range []string{"default", "prod"} {
		nad := &unstructured.Unstructured{}
		nad.SetGroupVersionKind(NetAttachDefGVK)
		err := fakeClient.Get(ctx, types.NamespacedName{Namespace: nsName, Name: "vlan-123"}, nad)
		if err != nil {
			t.Errorf("Expected NetworkAttachmentDefinition in namespace %q, but got error: %v", nsName, err)
			continue
		}

		cniConfig, found, _ := unstructured.NestedString(nad.Object, "spec", "config")
		if !found || cniConfig == "" {
			t.Errorf("NetworkAttachmentDefinition in %q missing spec.config", nsName)
		}
		if !strings.Contains(cniConfig, `"master": "gdcenet0.123"`) {
			t.Errorf("Expected master interface gdcenet0.123 in CNI config, got: %s", cniConfig)
		}
		if !strings.Contains(cniConfig, `"subnet": "172.16.12.0/24"`) {
			t.Errorf("Expected calculated subnet 172.16.12.0/24 in IPAM config, got: %s", cniConfig)
		}
		if !strings.Contains(cniConfig, `"gateway": "172.16.12.1"`) {
			t.Errorf("Expected gateway 172.16.12.1 in IPAM config, got: %s", cniConfig)
		}
	}

	// 2. Verify MetalLB IPAddressPool and L2Advertisement in kube-system
	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(IPAddressPoolGVK)
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "vlan-123-pool"}, pool)
	if err != nil {
		t.Fatalf("Expected MetalLB IPAddressPool in kube-system, got error: %v", err)
	}
	addresses, found, _ := unstructured.NestedSlice(pool.Object, "spec", "addresses")
	if !found || len(addresses) != 2 {
		t.Errorf("Expected 2 addresses in IPAddressPool, got %v", addresses)
	}

	l2Adv := &unstructured.Unstructured{}
	l2Adv.SetGroupVersionKind(L2AdvertisementGVK)
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "kube-system", Name: "l2advertise-vlan-123"}, l2Adv)
	if err != nil {
		t.Fatalf("Expected MetalLB L2Advertisement in kube-system, got error: %v", err)
	}

	// 3. Verify ClusterCIDRConfig
	cidrConfig := &unstructured.Unstructured{}
	cidrConfig.SetGroupVersionKind(ClusterCIDRConfigGVK)
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "vlan-123-cidr"}, cidrConfig)
	if err != nil {
		t.Fatalf("Expected ClusterCIDRConfig vlan-123-cidr, got error: %v", err)
	}
	cidr, found, _ := unstructured.NestedString(cidrConfig.Object, "spec", "ipv4", "cidr")
	if !found || cidr != "10.12.0.0/22" {
		t.Errorf("Expected ClusterCIDRConfig cidr 10.12.0.0/22, got %q", cidr)
	}
	mask, found, _ := unstructured.NestedInt64(cidrConfig.Object, "spec", "ipv4", "perNodeMaskSize")
	if !found || mask != 24 {
		t.Errorf("Expected perNodeMaskSize 24, got %d", mask)
	}

	// 4. Verify Network status conditions
	updatedNet := &unstructured.Unstructured{}
	updatedNet.SetGroupVersionKind(NetworkGVK)
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "vlan-123"}, updatedNet)
	conditions, found, _ := unstructured.NestedSlice(updatedNet.Object, "status", "conditions")
	if !found || len(conditions) < 2 {
		t.Errorf("Expected at least 2 status conditions on Network, got %v", conditions)
	}
}

func TestNetworkReconciler_Reconcile_CustomInterfaceAndNoGateway(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "custom-net",
			},
			"spec": map[string]interface{}{
				"nodeInterfaceMatcher": map[string]interface{}{
					"interfaceName": "eth1.500",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, netObj).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "custom-net"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	nad := &unstructured.Unstructured{}
	nad.SetGroupVersionKind(NetAttachDefGVK)
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "custom-net"}, nad)
	if err != nil {
		t.Fatalf("Expected NetAttachDef in default namespace: %v", err)
	}

	cniConfig, _, _ := unstructured.NestedString(nad.Object, "spec", "config")
	if !strings.Contains(cniConfig, `"master": "eth1.500"`) {
		t.Errorf("Expected master eth1.500, got: %s", cniConfig)
	}
	if !strings.Contains(cniConfig, `"subnet": "usePodCidr"`) {
		t.Errorf("Expected fallback subnet usePodCidr, got: %s", cniConfig)
	}
}

func TestNetworkReconciler_Reconcile_DeletionFinalizer(t *testing.T) {
	scheme := setupNetworkTestScheme()

	now := metav1.Now()
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name":              "deleted-net",
				"deletionTimestamp": now.Rfc3339Copy().Format(time.RFC3339),
				"finalizers":        []interface{}{NetworkFinalizer},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(netObj).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "deleted-net"}})
	if err != nil {
		t.Fatalf("Reconcile returned unexpected error during deletion: %v", err)
	}

	updatedNet := &unstructured.Unstructured{}
	updatedNet.SetGroupVersionKind(NetworkGVK)
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "deleted-net"}, updatedNet)
	if err == nil {
		finalizers := updatedNet.GetFinalizers()
		for _, f := range finalizers {
			if f == NetworkFinalizer {
				t.Errorf("Expected finalizer %q to be removed, but it was still present", NetworkFinalizer)
			}
		}
	}
}

func TestNetworkReconciler_Reconcile_TerminatingNamespaceSkipped(t *testing.T) {
	scheme := setupNetworkTestScheme()

	now := metav1.Now()
	terminatingNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "terminating-ns",
			DeletionTimestamp: &now,
			Finalizers:        []string{"kubernetes"},
		},
	}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-555",
				"annotations": map[string]interface{}{
					AnnotationVLANID: "555",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(terminatingNS, netObj).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-555"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	nad := &unstructured.Unstructured{}
	nad.SetGroupVersionKind(NetAttachDefGVK)
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "terminating-ns", Name: "vlan-555"}, nad)
	if err == nil {
		t.Errorf("Expected NetAttachDef NOT to be created in terminating namespace")
	}
}
