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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestMutatePodInterfaces_WithSecondaryNetwork(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationGKEInterfaces: `[{"interfaceName":"eth0","network":"pod-network"},{"interfaceName":"eth1","network":"vlan-123"}]`,
			},
		},
	}

	mutated := MutatePodInterfaces(pod)
	if !mutated {
		t.Fatalf("Expected MutatePodInterfaces to return true for pod with secondary network")
	}

	// 1. Check Multus annotation was injected
	multusRaw, hasMultus := pod.Annotations[AnnotationMultusNetworks]
	if !hasMultus {
		t.Fatalf("Expected %s annotation to be set", AnnotationMultusNetworks)
	}
	var multusList []MultusNetworkSpec
	if err := json.Unmarshal([]byte(multusRaw), &multusList); err != nil {
		t.Fatalf("Failed to unmarshal Multus annotation: %v", err)
	}
	if len(multusList) != 1 || multusList[0].Name != "vlan-123" || multusList[0].Interface != "eth1" {
		t.Errorf("Unexpected Multus list: %+v", multusList)
	}

	// 2. Check GKE interfaces was sanitized to primary only
	gkeRaw := pod.Annotations[AnnotationGKEInterfaces]
	var gkeList []GKEInterfaceSpec
	if err := json.Unmarshal([]byte(gkeRaw), &gkeList); err != nil {
		t.Fatalf("Failed to unmarshal GKE interfaces annotation: %v", err)
	}
	if len(gkeList) != 1 || gkeList[0].Network != "pod-network" || gkeList[0].InterfaceName != "eth0" {
		t.Errorf("Expected only primary interface in GKE annotation, got: %+v", gkeList)
	}

	// 3. Check default-interface
	if pod.Annotations[AnnotationGKEDefaultInterface] != "eth0" {
		t.Errorf("Expected default-interface to be eth0, got: %s", pod.Annotations[AnnotationGKEDefaultInterface])
	}
}

func TestMutatePodInterfaces_MultipleSecondaryNetworks(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationGKEInterfaces: `[{"interfaceName":"eth0","network":"pod-network"},{"interfaceName":"eth1","network":"vlan-123"},{"interfaceName":"eth2","network":"vlan-456"}]`,
			},
		},
	}

	mutated := MutatePodInterfaces(pod)
	if !mutated {
		t.Fatalf("Expected MutatePodInterfaces to return true")
	}

	multusRaw := pod.Annotations[AnnotationMultusNetworks]
	var multusList []MultusNetworkSpec
	if err := json.Unmarshal([]byte(multusRaw), &multusList); err != nil {
		t.Fatalf("Failed to unmarshal Multus annotation: %v", err)
	}
	if len(multusList) != 2 {
		t.Fatalf("Expected 2 Multus entries, got: %+v", multusList)
	}
	if multusList[0].Name != "vlan-123" || multusList[1].Name != "vlan-456" {
		t.Errorf("Unexpected Multus list order/content: %+v", multusList)
	}
}

func TestMutatePodInterfaces_ExistingMultusAnnotationMerged(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationMultusNetworks: `[{"name":"existing-net","interface":"eth10"}]`,
				AnnotationGKEInterfaces:  `[{"interfaceName":"eth0","network":"pod-network"},{"interfaceName":"eth1","network":"vlan-123"}]`,
			},
		},
	}

	mutated := MutatePodInterfaces(pod)
	if !mutated {
		t.Fatalf("Expected MutatePodInterfaces to return true")
	}

	var multusList []MultusNetworkSpec
	if err := json.Unmarshal([]byte(pod.Annotations[AnnotationMultusNetworks]), &multusList); err != nil {
		t.Fatalf("Failed to unmarshal Multus: %v", err)
	}
	if len(multusList) != 2 {
		t.Fatalf("Expected 2 Multus entries (existing merged with new), got: %+v", multusList)
	}
	if multusList[0].Name != "existing-net" || multusList[1].Name != "vlan-123" {
		t.Errorf("Unexpected merged Multus list: %+v", multusList)
	}
}

func TestMutatePodInterfaces_OnlyPrimaryNetworkUntouched(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationGKEInterfaces: `[{"interfaceName":"eth0","network":"pod-network"}]`,
			},
		},
	}

	mutated := MutatePodInterfaces(pod)
	if mutated {
		t.Fatalf("Expected MutatePodInterfaces to return false for primary-only pod")
	}
	if _, hasMultus := pod.Annotations[AnnotationMultusNetworks]; hasMultus {
		t.Errorf("Expected Multus annotation NOT to be added")
	}
}

func TestMutatePodInterfaces_NoAnnotationsOrInvalidJSON(t *testing.T) {
	podNoAnnotations := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1"}}
	if MutatePodInterfaces(podNoAnnotations) {
		t.Errorf("Expected false for pod with nil annotations")
	}

	podInvalidJSON := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod2",
			Annotations: map[string]string{
				AnnotationGKEInterfaces: "invalid-json-string",
			},
		},
	}
	if MutatePodInterfaces(podInvalidJSON) {
		t.Errorf("Expected false for pod with invalid json annotation")
	}
}

func TestMutatePodInterfacesWithContext_DynamicIPAMAllocation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-456",
				"annotations": map[string]interface{}{
					AnnotationLBServiceVIPs: `["192.168.45.200-192.168.45.250"]`,
				},
			},
			"spec": map[string]interface{}{
				"gateway4": "192.168.45.1",
				"l2NetworkConfig": map[string]interface{}{
					"prefixLength4": int64(24),
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(netObj).Build()
	ctx := context.Background()

	// 1. First Pod created
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationGKEInterfaces: `[{"interfaceName":"eth0","network":"pod-network"},{"interfaceName":"eth1","network":"vlan-456"}]`,
			},
		},
	}

	mutated, err := MutatePodInterfacesWithContext(ctx, fakeClient, pod1)
	if err != nil || !mutated {
		t.Fatalf("MutatePodInterfacesWithContext failed on pod1: %v", err)
	}

	var multus1 []MultusNetworkSpec
	if err := json.Unmarshal([]byte(pod1.Annotations[AnnotationMultusNetworks]), &multus1); err != nil {
		t.Fatalf("Failed to unmarshal Multus for pod1: %v", err)
	}
	if len(multus1) != 1 || len(multus1[0].IPs) != 1 || multus1[0].IPs[0] != "192.168.45.10/24" {
		t.Fatalf("Expected pod1 to get 192.168.45.10/24, got: %+v", multus1)
	}

	// Save pod1 to client
	if err := fakeClient.Create(ctx, pod1); err != nil {
		t.Fatalf("Failed to save pod1: %v", err)
	}

	// 2. Second Pod created (on separate node or namespace)
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-2",
			Namespace: "tenant-a",
			Annotations: map[string]string{
				AnnotationGKEInterfaces: `[{"interfaceName":"eth0","network":"pod-network"},{"interfaceName":"eth1","network":"vlan-456"}]`,
			},
		},
	}

	mutated2, err := MutatePodInterfacesWithContext(ctx, fakeClient, pod2)
	if err != nil || !mutated2 {
		t.Fatalf("MutatePodInterfacesWithContext failed on pod2: %v", err)
	}

	var multus2 []MultusNetworkSpec
	if err := json.Unmarshal([]byte(pod2.Annotations[AnnotationMultusNetworks]), &multus2); err != nil {
		t.Fatalf("Failed to unmarshal Multus for pod2: %v", err)
	}
	if len(multus2) != 1 || len(multus2[0].IPs) != 1 || multus2[0].IPs[0] != "192.168.45.11/24" {
		t.Fatalf("Expected pod2 to get distinct IP 192.168.45.11/24, got: %+v", multus2)
	}
}

func TestMutatePodInterfacesWithContext_PreservesExplicitStaticIP(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-456",
			},
			"spec": map[string]interface{}{
				"gateway4": "192.168.45.1",
				"l2NetworkConfig": map[string]interface{}{
					"prefixLength4": int64(24),
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(netObj).Build()
	ctx := context.Background()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-explicit",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationMultusNetworks: `[{"name":"vlan-456","interface":"eth1","ips":["192.168.45.99/24"]}]`,
				AnnotationGKEInterfaces:  `[{"interfaceName":"eth0","network":"pod-network"},{"interfaceName":"eth1","network":"vlan-456"}]`,
			},
		},
	}

	mutated, err := MutatePodInterfacesWithContext(ctx, fakeClient, pod)
	if err != nil || !mutated {
		t.Fatalf("MutatePodInterfacesWithContext failed: %v", err)
	}

	var multus []MultusNetworkSpec
	if err := json.Unmarshal([]byte(pod.Annotations[AnnotationMultusNetworks]), &multus); err != nil {
		t.Fatalf("Failed to unmarshal Multus: %v", err)
	}
	if len(multus) != 1 || len(multus[0].IPs) != 1 || multus[0].IPs[0] != "192.168.45.99/24" {
		t.Fatalf("Expected explicit IP 192.168.45.99/24 to be preserved, got: %+v", multus)
	}
}

func TestGenerateWebhookCerts(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "webhook-test-certs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	caPEM, err := GenerateWebhookCerts(tempDir, "10.10.0.2")
	if err != nil {
		t.Fatalf("GenerateWebhookCerts failed: %v", err)
	}
	if len(caPEM) == 0 || !strings.Contains(string(caPEM), "BEGIN CERTIFICATE") {
		t.Fatalf("Expected valid CA PEM bytes, got: %s", string(caPEM))
	}

	certBytes, err := os.ReadFile(filepath.Join(tempDir, "tls.crt"))
	if err != nil || len(certBytes) == 0 {
		t.Fatalf("Expected valid tls.crt file: %v", err)
	}

	keyBytes, err := os.ReadFile(filepath.Join(tempDir, "tls.key"))
	if err != nil || len(keyBytes) == 0 {
		t.Fatalf("Expected valid tls.key file: %v", err)
	}
}

func TestEnsureMutatingWebhookConfiguration(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = admissionregistrationv1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	caBundle := []byte("test-ca-bundle")
	webhookURL := "https://10.10.0.2:9443/mutate-pod"

	// 1. First invocation creates the MutatingWebhookConfiguration
	err := EnsureMutatingWebhookConfiguration(ctx, fakeClient, webhookURL, caBundle)
	if err != nil {
		t.Fatalf("EnsureMutatingWebhookConfiguration failed: %v", err)
	}

	created := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: MutatingWebhookConfigName}, created); err != nil {
		t.Fatalf("Expected MutatingWebhookConfiguration to exist: %v", err)
	}
	if len(created.Webhooks) != 1 {
		t.Fatalf("Expected 1 webhook rule, got %d", len(created.Webhooks))
	}
	if *created.Webhooks[0].ClientConfig.URL != webhookURL {
		t.Errorf("Expected URL %s, got %s", webhookURL, *created.Webhooks[0].ClientConfig.URL)
	}

	// 2. Second invocation updates the existing MutatingWebhookConfiguration idempotently
	newURL := "https://10.10.0.2:9444/mutate-pod"
	err = EnsureMutatingWebhookConfiguration(ctx, fakeClient, newURL, caBundle)
	if err != nil {
		t.Fatalf("Second EnsureMutatingWebhookConfiguration failed: %v", err)
	}

	updated := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: MutatingWebhookConfigName}, updated); err != nil {
		t.Fatalf("Expected updated config to exist: %v", err)
	}
	if *updated.Webhooks[0].ClientConfig.URL != newURL {
		t.Errorf("Expected updated URL %s, got %s", newURL, *updated.Webhooks[0].ClientConfig.URL)
	}
}

func TestPodInterfaceMutator_Handle(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	netObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]interface{}{
				"name": "vlan-123",
			},
			"spec": map[string]interface{}{
				"gateway4": "172.16.12.1",
				"l2NetworkConfig": map[string]interface{}{
					"prefixLength4": int64(24),
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(netObj).Build()
	decoder := admission.NewDecoder(scheme)

	mutator := &PodInterfaceMutator{
		Client:  fakeClient,
		Decoder: decoder,
		Log:     logr.Discard(),
	}

	ctx := context.Background()

	// 1. Valid pod with secondary network -> returns patch response
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webhook-pod",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationGKEInterfaces: `[{"interfaceName":"eth1","network":"vlan-123"}]`,
			},
		},
	}
	rawPod, _ := json.Marshal(pod)
	resp := mutator.Handle(ctx, admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object: runtime.RawExtension{Raw: rawPod},
		},
	})
	if !resp.Allowed {
		t.Fatalf("Expected admission allowed for valid pod, got result: %+v", resp.Result)
	}
	if len(resp.Patches) == 0 {
		t.Errorf("Expected non-empty JSON patches for mutated pod")
	}

	// 2. Valid pod with no secondary networks -> allowed without patches
	primaryPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary-pod",
			Namespace: "default",
		},
	}
	rawPrimary, _ := json.Marshal(primaryPod)
	respPrimary := mutator.Handle(ctx, admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object: runtime.RawExtension{Raw: rawPrimary},
		},
	})
	if !respPrimary.Allowed || len(respPrimary.Patches) != 0 {
		t.Errorf("Expected allowed with 0 patches for primary-only pod, got Allowed=%v Patches=%d", respPrimary.Allowed, len(respPrimary.Patches))
	}

	// 3. Invalid JSON -> 400 Bad Request
	respBad := mutator.Handle(ctx, admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object: runtime.RawExtension{Raw: []byte("{invalid-json")},
		},
	})
	if respBad.Allowed || respBad.Result == nil || respBad.Result.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 BadRequest for invalid JSON, got: %+v", respBad)
	}

	// 4. Missing Network CR -> 500 Internal Server Error
	missingNetPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "missing-net-pod",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationGKEInterfaces: `[{"interfaceName":"eth1","network":"nonexistent-vlan"}]`,
			},
		},
	}
	rawMissing, _ := json.Marshal(missingNetPod)
	respMissing := mutator.Handle(ctx, admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object: runtime.RawExtension{Raw: rawMissing},
		},
	})
	if respMissing.Allowed || respMissing.Result == nil || respMissing.Result.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 InternalServerError when Network CR is missing, got: %+v", respMissing)
	}
}

func TestExcludeIPRangeOrCIDR_AndIPHelpers(t *testing.T) {
	excluded := make(map[string]bool)

	// Empty and invalid strings should be ignored
	excludeIPRangeOrCIDR(excluded, "")
	excludeIPRangeOrCIDR(excluded, "   ")
	excludeIPRangeOrCIDR(excluded, "invalid-ip")
	excludeIPRangeOrCIDR(excluded, "10.0.0.10-10.0.0.5") // inverted range
	excludeIPRangeOrCIDR(excluded, "10.0.0.1-invalid")
	excludeIPRangeOrCIDR(excluded, "invalid/24")
	excludeIPRangeOrCIDR(excluded, "2001:db8::1")
	if len(excluded) != 0 {
		t.Errorf("Expected 0 excluded IPs for invalid inputs, got %v", excluded)
	}

	// Valid IP range
	excludeIPRangeOrCIDR(excluded, "192.168.1.10-192.168.1.12")
	for _, ip := range []string{"192.168.1.10", "192.168.1.11", "192.168.1.12"} {
		if !excluded[ip] {
			t.Errorf("Expected %s to be excluded from range", ip)
		}
	}

	// Valid CIDR (/30 has 4 addresses: .240, .241, .242, .243)
	excludeIPRangeOrCIDR(excluded, "192.168.1.240/30")
	for _, ip := range []string{"192.168.1.240", "192.168.1.241", "192.168.1.242", "192.168.1.243"} {
		if !excluded[ip] {
			t.Errorf("Expected %s to be excluded from CIDR", ip)
		}
	}

	// Boundary IPv4 255.255.255.255 must not wrap uint32 in an infinite loop
	excludeIPRangeOrCIDR(excluded, "255.255.255.254-255.255.255.255")
	if !excluded["255.255.255.255"] {
		t.Errorf("Expected 255.255.255.255 to be excluded without uint32 wraparound")
	}

	// Valid single IP
	excludeIPRangeOrCIDR(excluded, "192.168.1.50")
	if !excluded["192.168.1.50"] {
		t.Errorf("Expected single IP 192.168.1.50 to be excluded")
	}

	// ipToUint32 with IPv6 returns 0
	if got := ipToUint32(net.ParseIP("2001:db8::1")); got != 0 {
		t.Errorf("Expected ipToUint32 to return 0 for IPv6, got %d", got)
	}
}

func TestAllocateSecondaryIP_GatewayCIDRsAndExhaustion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// /28 subnet (192.168.45.0/28 -> .0 to .15; .1 gw, .2-.9 host nodes, .15 broadcast -> leaves .10-.14)
	netObj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "Network",
			"metadata": map[string]any{
				"name": "vlan-small",
			},
			"spec": map[string]any{
				"gateway4": "192.168.45.1",
				"l2NetworkConfig": map[string]any{
					"prefixLength4": int64(28),
				},
			},
		},
	}

	gwCIDR := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.gke.io/v1",
			"kind":       "GKEGatewayCIDR",
			"metadata": map[string]any{
				"name": "gw-cidr-small",
			},
			"spec": map[string]any{
				"network": "vlan-small",
				"ip4cidr": "192.168.45.12/30", // excludes .12, .13, .14, .15
			},
		},
	}

	// Pod occupying .10 via network-status annotation
	activePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-pod",
			Namespace: "default",
			Annotations: map[string]string{
				"k8s.v1.cni.cncf.io/network-status": `[{"name":"default/vlan-small","ips":["192.168.45.10"]}]`,
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	// Succeeded pod occupying .11 should be ignored
	succeededPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "succeeded-pod",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationMultusNetworks: `[{"name":"vlan-small","ips":["192.168.45.11/28"]}]`,
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(netObj, gwCIDR, activePod, succeededPod).
		Build()

	ctx := context.Background()
	inUse := make(map[string]bool)

	// .10 is used by activePod, .12-.14 excluded by GKEGatewayCIDR, so .11 (from succeededPod) must be allocated
	ip, err := allocateSecondaryIP(ctx, fakeClient, "vlan-small", inUse)
	if err != nil {
		t.Fatalf("Expected .11/28 to be allocated, got error: %v", err)
	}
	if ip != "192.168.45.11/28" {
		t.Errorf("Expected 192.168.45.11/28, got %s", ip)
	}

	// Next allocation should fail with IP pool exhausted
	_, err = allocateSecondaryIP(ctx, fakeClient, "vlan-small", inUse)
	if err == nil || !strings.Contains(err.Error(), "IP pool exhausted") {
		t.Errorf("Expected IP pool exhausted error, got: %v", err)
	}
}

func TestGenerateWebhookCerts_DNSAndError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "webhook-dns-certs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Hostname (non-IP) branch
	caPEM, err := GenerateWebhookCerts(tempDir, "webhook.gem.internal")
	if err != nil || len(caPEM) == 0 {
		t.Fatalf("Expected GenerateWebhookCerts to succeed for DNS host, got err: %v", err)
	}

	// Error branch when certDir cannot be created (under /dev/null)
	if _, err := GenerateWebhookCerts("/dev/null/invalid-dir", "10.10.0.2"); err == nil {
		t.Errorf("Expected error when certDir is under /dev/null")
	}
}
