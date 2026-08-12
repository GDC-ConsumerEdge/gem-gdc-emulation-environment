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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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

	// Define a complete Network custom resource with all GEM annotations
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
				"annotations": map[string]interface{}{
					AnnotationVLANID:        "123",
					AnnotationVLANMTU:       "1410",
					AnnotationLBServiceVIPs: `["172.16.12.200", "172.16.12.201"]`,
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
		WithObjects(nsDefault, nsProd, nsKubeSystem, netObj,
			provisionedNetworksCM(map[string]string{"123": "gdcenet0.123"})).
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

	// 3. Verify Network status conditions
	updatedNet := &unstructured.Unstructured{}
	updatedNet.SetGroupVersionKind(NetworkGVK)
	_ = fakeClient.Get(ctx, types.NamespacedName{Name: "vlan-123"}, updatedNet)
	conditions, found, _ := unstructured.NestedSlice(updatedNet.Object, "status", "conditions")
	if !found || len(conditions) == 0 {
		t.Fatalf("Expected status conditions on Network, got %v", conditions)
	}
	for _, c := range conditions {
		if cMap, ok := c.(map[string]interface{}); ok && cMap["type"] == "Ready" {
			if cMap["status"] != "True" {
				t.Errorf("Expected Ready=True for a fully provisioned Network, got status=%v reason=%v",
					cMap["status"], cMap["reason"])
			}
		}
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

// provisionedNetworksCM builds the ConfigMap Ansible publishes at cluster build time,
// enumerating the VLANs whose host VXLAN interfaces were actually provisioned.
func provisionedNetworksCM(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ProvisionedNetworksConfigMap,
			Namespace: "kube-system",
		},
		Data: data,
	}
}

func TestNetworkReconciler_Reconcile_MissingHostInterfaceReportsNotReady(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-999",
				"annotations": map[string]interface{}{
					AnnotationVLANID: "999",
				},
			},
		},
	}

	// No provisioned-networks ConfigMap exists at all: nothing was provisioned by Ansible.
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
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-999"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedNet := &unstructured.Unstructured{}
	updatedNet.SetGroupVersionKind(NetworkGVK)
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "vlan-999"}, updatedNet); err != nil {
		t.Fatalf("Failed to get Network: %v", err)
	}

	conditions, found, _ := unstructured.NestedSlice(updatedNet.Object, "status", "conditions")
	if !found {
		t.Fatalf("Expected status conditions on Network")
	}
	var readyCond map[string]interface{}
	for _, c := range conditions {
		if cMap, ok := c.(map[string]interface{}); ok && cMap["type"] == "Ready" {
			readyCond = cMap
		}
	}
	if readyCond == nil {
		t.Fatalf("Expected a Ready condition, got %v", conditions)
	}
	if readyCond["status"] != "False" || readyCond["reason"] != "MissingHostInterface" {
		t.Errorf("Expected Ready=False with reason MissingHostInterface for unprovisioned network, got status=%v reason=%v",
			readyCond["status"], readyCond["reason"])
	}
}

func TestNetworkReconciler_Reconcile_ProvisionedHostInterfaceReportsReady(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
				"annotations": map[string]interface{}{
					AnnotationVLANID: "123",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, netObj, provisionedNetworksCM(map[string]string{"123": "gdcenet0.123"})).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-123"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedNet := &unstructured.Unstructured{}
	updatedNet.SetGroupVersionKind(NetworkGVK)
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "vlan-123"}, updatedNet); err != nil {
		t.Fatalf("Failed to get Network: %v", err)
	}

	conditions, _, _ := unstructured.NestedSlice(updatedNet.Object, "status", "conditions")
	var readyCond map[string]interface{}
	for _, c := range conditions {
		if cMap, ok := c.(map[string]interface{}); ok && cMap["type"] == "Ready" {
			readyCond = cMap
		}
	}
	if readyCond == nil {
		t.Fatalf("Expected a Ready condition, got %v", conditions)
	}
	if readyCond["status"] != "True" {
		t.Errorf("Expected Ready=True for provisioned network, got status=%v reason=%v",
			readyCond["status"], readyCond["reason"])
	}
}

func TestNetworkReconciler_Reconcile_MissingHostInterfaceEmitsWarningEvent(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-999",
				"annotations": map[string]interface{}{
					AnnotationVLANID: "999",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, netObj).
		Build()

	recorder := record.NewFakeRecorder(10)
	reconciler := &NetworkReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Log:      logr.Discard(),
		Recorder: recorder,
	}

	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-999"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	select {
	case ev := <-recorder.Events:
		if !strings.Contains(ev, "Warning") || !strings.Contains(ev, "MissingHostInterface") {
			t.Errorf("Expected Warning event with reason MissingHostInterface, got %q", ev)
		}
	default:
		t.Errorf("Expected a Warning event for unprovisioned network, but none was recorded")
	}
}

func TestNetworkReconciler_Reconcile_ChildResourcesHaveOwnerReference(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	nsKubeSystem := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
				"uid":  "test-uid-123",
				"annotations": map[string]interface{}{
					AnnotationVLANID:        "123",
					AnnotationLBServiceVIPs: `["172.16.12.200-172.16.12.250"]`,
				},
			},
			"spec": map[string]interface{}{
				"gateway4": "172.16.12.1",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, nsKubeSystem, netObj,
			provisionedNetworksCM(map[string]string{"123": "gdcenet0.123"})).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-123"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	assertOwnedByNetwork := func(t *testing.T, gvk schema.GroupVersionKind, ns, name string) {
		t.Helper()
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, obj); err != nil {
			t.Fatalf("Failed to get %s %s/%s: %v", gvk.Kind, ns, name, err)
		}
		for _, ref := range obj.GetOwnerReferences() {
			if ref.Kind == "Network" && ref.Name == "vlan-123" {
				return
			}
		}
		t.Errorf("Expected %s %s/%s to carry an OwnerReference to Network vlan-123, got %v",
			gvk.Kind, ns, name, obj.GetOwnerReferences())
	}

	assertOwnedByNetwork(t, NetAttachDefGVK, "default", "vlan-123")
	assertOwnedByNetwork(t, IPAddressPoolGVK, "kube-system", "vlan-123-pool")
	assertOwnedByNetwork(t, L2AdvertisementGVK, "kube-system", "l2advertise-vlan-123")
}

func TestNetworkReconciler_Reconcile_DeletionCleansUpChildren(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	nsKubeSystem := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
				"annotations": map[string]interface{}{
					AnnotationVLANID:        "123",
					AnnotationLBServiceVIPs: `["172.16.12.200-172.16.12.250"]`,
				},
			},
			"spec": map[string]interface{}{
				"gateway4": "172.16.12.1",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, nsKubeSystem, netObj,
			provisionedNetworksCM(map[string]string{"123": "gdcenet0.123"})).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-123"}}

	// First reconcile: adds the finalizer and creates child resources.
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("First reconcile returned error: %v", err)
	}

	nad := &unstructured.Unstructured{}
	nad.SetGroupVersionKind(NetAttachDefGVK)
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "vlan-123"}, nad); err != nil {
		t.Fatalf("Expected NAD to exist after first reconcile: %v", err)
	}

	// Delete the Network: the finalizer holds it, setting deletionTimestamp.
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(NetworkGVK)
	if err := fakeClient.Get(ctx, req.NamespacedName, current); err != nil {
		t.Fatalf("Failed to get Network: %v", err)
	}
	if err := fakeClient.Delete(ctx, current); err != nil {
		t.Fatalf("Failed to delete Network: %v", err)
	}

	// Second reconcile: the deletion path must clean up all child resources before releasing.
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("Deletion reconcile returned error: %v", err)
	}

	for _, check := range []struct {
		gvk  schema.GroupVersionKind
		ns   string
		name string
	}{
		{NetAttachDefGVK, "default", "vlan-123"},
		{IPAddressPoolGVK, "kube-system", "vlan-123-pool"},
		{L2AdvertisementGVK, "kube-system", "l2advertise-vlan-123"},
	} {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(check.gvk)
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: check.ns, Name: check.name}, obj); err == nil {
			t.Errorf("Expected %s %s/%s to be deleted with its Network, but it still exists", check.gvk.Kind, check.ns, check.name)
		}
	}
}

func TestNetworkReconciler_Reconcile_NoOpWhenUnchanged(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	nsKubeSystem := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
				"annotations": map[string]interface{}{
					AnnotationVLANID:        "123",
					AnnotationLBServiceVIPs: `["172.16.12.200-172.16.12.250"]`,
				},
			},
			"spec": map[string]interface{}{
				"gateway4": "172.16.12.1",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, nsKubeSystem, netObj,
			provisionedNetworksCM(map[string]string{"123": "gdcenet0.123"})).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-123"}}
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("First reconcile returned error: %v", err)
	}

	getRV := func(t *testing.T, gvk schema.GroupVersionKind, ns, name string) string {
		t.Helper()
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, obj); err != nil {
			t.Fatalf("Failed to get %s %s/%s: %v", gvk.Kind, ns, name, err)
		}
		return obj.GetResourceVersion()
	}

	nadRV := getRV(t, NetAttachDefGVK, "default", "vlan-123")
	poolRV := getRV(t, IPAddressPoolGVK, "kube-system", "vlan-123-pool")

	// Reconcile again with nothing changed: no child object should be rewritten.
	if _, err := reconciler.Reconcile(ctx, req); err != nil {
		t.Fatalf("Second reconcile returned error: %v", err)
	}

	if rv := getRV(t, NetAttachDefGVK, "default", "vlan-123"); rv != nadRV {
		t.Errorf("Expected NAD ResourceVersion unchanged on no-op reconcile, got %s -> %s", nadRV, rv)
	}
	if rv := getRV(t, IPAddressPoolGVK, "kube-system", "vlan-123-pool"); rv != poolRV {
		t.Errorf("Expected IPAddressPool ResourceVersion unchanged on no-op reconcile, got %s -> %s", poolRV, rv)
	}
}

func TestNetworkReconciler_Reconcile_ServiceBindingRequiresAllowlist(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	nsProd := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "prod"}}

	// Network restricted to the "prod" namespace only.
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
				"annotations": map[string]interface{}{
					AnnotationVLANID:            "123",
					AnnotationLBServiceVIPs:     `["172.16.12.200-172.16.12.250"]`,
					AnnotationAllowedNamespaces: "prod",
				},
			},
		},
	}

	deniedSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "denied-svc",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationNetworkTarget: "vlan-123",
			},
		},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
	}
	allowedSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allowed-svc",
			Namespace: "prod",
			Annotations: map[string]string{
				AnnotationNetworkTarget: "vlan-123",
			},
		},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, nsProd, netObj, deniedSvc, allowedSvc,
			provisionedNetworksCM(map[string]string{"123": "gdcenet0.123"})).
		Build()

	recorder := record.NewFakeRecorder(10)
	reconciler := &NetworkReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Log:      logr.Discard(),
		Recorder: recorder,
	}

	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-123"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// The Service outside the allowlist must NOT be bound to the pool.
	updatedDenied := &corev1.Service{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "denied-svc"}, updatedDenied); err != nil {
		t.Fatalf("Failed to get denied Service: %v", err)
	}
	if pool, ok := updatedDenied.Annotations["metallb.universe.tf/address-pool"]; ok {
		t.Errorf("Expected denied Service NOT to be bound to MetalLB pool, but found annotation %q", pool)
	}

	// A diagnosable signal must exist for the denied binding.
	select {
	case ev := <-recorder.Events:
		if !strings.Contains(ev, "Warning") {
			t.Errorf("Expected a Warning event for denied Service binding, got %q", ev)
		}
	default:
		t.Errorf("Expected an event recording the denied Service binding, but none was recorded")
	}

	// The Service in the allowed namespace must still bind.
	updatedAllowed := &corev1.Service{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "prod", Name: "allowed-svc"}, updatedAllowed); err != nil {
		t.Fatalf("Failed to get allowed Service: %v", err)
	}
	if pool := updatedAllowed.Annotations["metallb.universe.tf/address-pool"]; pool != "vlan-123-pool" {
		t.Errorf("Expected allowed Service to be bound to vlan-123-pool, got %q", pool)
	}
}

func TestNetworkReconciler_ApplyOrUpdate_RetriesOnConflict(t *testing.T) {
	scheme := setupNetworkTestScheme()

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(NetAttachDefGVK)
	existing.SetName("vlan-123")
	existing.SetNamespace("default")
	existing.Object["spec"] = map[string]interface{}{"config": "old-config"}

	// Fail the first Update with a Conflict (as if another writer raced us), succeed afterwards.
	updateCalls := 0
	conflictOnce := interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			updateCalls++
			if updateCalls == 1 {
				return apierrors.NewConflict(
					schema.GroupResource{Group: "k8s.cni.cncf.io", Resource: "network-attachment-definitions"},
					obj.GetName(), errors.New("simulated conflict"))
			}
			return c.Update(ctx, obj, opts...)
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existing).
		WithInterceptorFuncs(conflictOnce).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	desired := &unstructured.Unstructured{}
	desired.SetGroupVersionKind(NetAttachDefGVK)
	desired.SetName("vlan-123")
	desired.SetNamespace("default")
	desired.Object["spec"] = map[string]interface{}{"config": "new-config"}

	ctx := context.Background()
	if err := reconciler.applyOrUpdate(ctx, desired); err != nil {
		t.Fatalf("Expected applyOrUpdate to retry through the conflict and succeed, got: %v", err)
	}

	final := &unstructured.Unstructured{}
	final.SetGroupVersionKind(NetAttachDefGVK)
	if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "vlan-123"}, final); err != nil {
		t.Fatalf("Failed to get object after applyOrUpdate: %v", err)
	}
	if cfg, _, _ := unstructured.NestedString(final.Object, "spec", "config"); cfg != "new-config" {
		t.Errorf("Expected object to reflect desired spec after conflict retry, got config=%q", cfg)
	}
	if updateCalls < 2 {
		t.Errorf("Expected at least 2 Update attempts (conflict then retry), got %d", updateCalls)
	}
}

func TestNetworkReconciler_Reconcile_ChildResourceErrorReflectedInStatus(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
				"annotations": map[string]interface{}{
					AnnotationVLANID: "123",
				},
			},
			"spec": map[string]interface{}{
				"gateway4": "172.16.12.1",
			},
		},
	}

	// Fail every NetworkAttachmentDefinition create so the child-resource reconcile step errors.
	failNADCreate := interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if obj.GetObjectKind().GroupVersionKind().Kind == "NetworkAttachmentDefinition" {
				return errors.New("simulated NAD create failure")
			}
			return c.Create(ctx, obj, opts...)
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, netObj, provisionedNetworksCM(map[string]string{"123": "gdcenet0.123"})).
		WithInterceptorFuncs(failNADCreate).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-123"}}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedNet := &unstructured.Unstructured{}
	updatedNet.SetGroupVersionKind(NetworkGVK)
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: "vlan-123"}, updatedNet); err != nil {
		t.Fatalf("Failed to get Network: %v", err)
	}

	conditions, _, _ := unstructured.NestedSlice(updatedNet.Object, "status", "conditions")
	var readyCond map[string]interface{}
	for _, c := range conditions {
		if cMap, ok := c.(map[string]interface{}); ok && cMap["type"] == "Ready" {
			readyCond = cMap
		}
	}
	if readyCond == nil {
		t.Fatalf("Expected a Ready condition, got %v", conditions)
	}
	if readyCond["status"] != "False" || readyCond["reason"] != "ChildResourceError" {
		t.Errorf("Expected Ready=False/ChildResourceError when a child resource fails, got status=%v reason=%v",
			readyCond["status"], readyCond["reason"])
	}
	if msg, _ := readyCond["message"].(string); !strings.Contains(msg, "NetworkAttachmentDefinition") {
		t.Errorf("Expected Ready message to identify the failed child resource, got %q", msg)
	}
}

func TestNetworkReconciler_Reconcile_CoreDNSReadyDerivedFromCorefile(t *testing.T) {
	scheme := setupNetworkTestScheme()

	buildNet := func() *unstructured.Unstructured {
		return &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "networking.gke.io/v1",
				"kind":       "Network",
				"metadata": map[string]interface{}{
					"name": "vlan-123",
					"annotations": map[string]interface{}{
						AnnotationVLANID: "123",
					},
				},
			},
		}
	}

	getCoreDNSCond := func(t *testing.T, c client.Client) map[string]interface{} {
		t.Helper()
		updatedNet := &unstructured.Unstructured{}
		updatedNet.SetGroupVersionKind(NetworkGVK)
		if err := c.Get(context.Background(), types.NamespacedName{Name: "vlan-123"}, updatedNet); err != nil {
			t.Fatalf("Failed to get Network: %v", err)
		}
		conditions, _, _ := unstructured.NestedSlice(updatedNet.Object, "status", "conditions")
		for _, cond := range conditions {
			if cMap, ok := cond.(map[string]interface{}); ok && cMap["type"] == "CoreDNSReady" {
				return cMap
			}
		}
		return nil
	}

	t.Run("rule present reports true", func(t *testing.T) {
		corednsCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "coredns-config", Namespace: "kube-system"},
			Data: map[string]string{
				"Corefile": ".:53 {\n    ready\n    rewrite name suffix .gkegw.cluster.local .svc.cluster.local\n    kubernetes cluster.local\n}",
			},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(buildNet(), corednsCM, provisionedNetworksCM(map[string]string{"123": "gdcenet0.123"})).
			Build()
		reconciler := &NetworkReconciler{Client: fakeClient, Scheme: scheme, Log: logr.Discard()}
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-123"}}); err != nil {
			t.Fatalf("Reconcile returned error: %v", err)
		}
		cond := getCoreDNSCond(t, fakeClient)
		if cond == nil || cond["status"] != "True" {
			t.Errorf("Expected CoreDNSReady=True when the rewrite rule is present, got %v", cond)
		}
	})

	t.Run("rule absent reports false", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(buildNet(), provisionedNetworksCM(map[string]string{"123": "gdcenet0.123"})).
			Build()
		reconciler := &NetworkReconciler{Client: fakeClient, Scheme: scheme, Log: logr.Discard()}
		if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-123"}}); err != nil {
			t.Fatalf("Reconcile returned error: %v", err)
		}
		cond := getCoreDNSCond(t, fakeClient)
		if cond == nil || cond["status"] != "False" {
			t.Errorf("Expected CoreDNSReady=False when no CoreDNS rewrite rule exists, got %v", cond)
		}
	})
}

func TestNetworkReconciler_Reconcile_ServiceBinding(t *testing.T) {
	scheme := setupNetworkTestScheme()

	nsDefault := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
				"annotations": map[string]interface{}{
					AnnotationVLANID:        "123",
					AnnotationLBServiceVIPs: `["172.16.12.200-172.16.12.250"]`,
				},
			},
		},
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-lb-svc",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationNetworkTarget: "vlan-123",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nsDefault, netObj, svc).
		Build()

	reconciler := &NetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Log:    logr.Discard(),
	}

	ctx := context.Background()
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "vlan-123"}})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	updatedSvc := &corev1.Service{}
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-lb-svc"}, updatedSvc)
	if err != nil {
		t.Fatalf("Failed to get updated Service: %v", err)
	}

	poolAnnotation := updatedSvc.Annotations["metallb.universe.tf/address-pool"]
	if poolAnnotation != "vlan-123-pool" {
		t.Errorf("Expected metallb.universe.tf/address-pool: vlan-123-pool, got %q", poolAnnotation)
	}
}
