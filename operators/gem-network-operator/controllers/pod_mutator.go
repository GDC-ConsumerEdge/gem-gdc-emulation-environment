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
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	AnnotationGKEInterfaces       = "networking.gke.io/interfaces"
	AnnotationGKEDefaultInterface = "networking.gke.io/default-interface"
	AnnotationMultusNetworks      = "k8s.v1.cni.cncf.io/networks"
)

type GKEInterfaceSpec struct {
	InterfaceName string `json:"interfaceName"`
	Network       string `json:"network"`
}

type MultusNetworkSpec struct {
	Name      string   `json:"name"`
	Interface string   `json:"interface,omitempty"`
	IPs       []string `json:"ips,omitempty"`
}

// PodInterfaceMutator mutates Pod definitions carrying GDC Connected networking.gke.io/interfaces
// annotations into Multus k8s.v1.cni.cncf.io/networks annotations with dynamic IPAM allocation,
// allowing unmodified GDC manifests to attach secondary networks seamlessly in GEM without IP collisions.
type PodInterfaceMutator struct {
	Client  client.Client
	Decoder admission.Decoder
	Log     logr.Logger
}

func (m *PodInterfaceMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := m.Decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	mutated, err := MutatePodInterfacesWithContext(ctx, m.Client, pod)
	if err != nil {
		m.Log.Error(err, "Failed to mutate pod interfaces and allocate IPAM", "pod", pod.Name, "namespace", pod.Namespace)
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if !mutated {
		return admission.Allowed("no secondary network interfaces to mutate")
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (m *PodInterfaceMutator) InjectDecoder(d admission.Decoder) error {
	m.Decoder = d
	return nil
}

// MutatePodInterfaces inspects a Pod's networking.gke.io/interfaces annotation without context.
// For backwards compatibility and unit testing where no Kubernetes client is available.
func MutatePodInterfaces(pod *corev1.Pod) bool {
	mutated, _ := MutatePodInterfacesWithContext(context.Background(), nil, pod)
	return mutated
}

// MutatePodInterfacesWithContext inspects a Pod's networking.gke.io/interfaces annotation. If secondary
// network entries are found, it dynamically allocates a unique IPAM address, injects matching
// k8s.v1.cni.cncf.io/networks entries for Multus, and sanitizes networking.gke.io/interfaces so
// Cilium only configures the primary network interface.
func MutatePodInterfacesWithContext(ctx context.Context, c client.Client, pod *corev1.Pod) (bool, error) {
	if pod.Annotations == nil {
		return false, nil
	}

	rawGKEInterfaces, ok := pod.Annotations[AnnotationGKEInterfaces]
	if !ok || strings.TrimSpace(rawGKEInterfaces) == "" {
		return false, nil
	}

	var gkeIfaces []GKEInterfaceSpec
	if err := json.Unmarshal([]byte(rawGKEInterfaces), &gkeIfaces); err != nil {
		return false, nil
	}

	var primaryIfaces []GKEInterfaceSpec
	var secondaryNetworks []MultusNetworkSpec

	for _, iface := range gkeIfaces {
		netName := strings.TrimSpace(iface.Network)
		if netName == "" || netName == "pod-network" || netName == "default" {
			primaryIfaces = append(primaryIfaces, iface)
		} else {
			ifaceName := strings.TrimSpace(iface.InterfaceName)
			secondaryNetworks = append(secondaryNetworks, MultusNetworkSpec{
				Name:      netName,
				Interface: ifaceName,
			})
		}
	}

	if len(secondaryNetworks) == 0 {
		return false, nil
	}

	// Parse existing Multus networks if already specified
	var existingMultus []MultusNetworkSpec
	if rawMultus, hasMultus := pod.Annotations[AnnotationMultusNetworks]; hasMultus && strings.TrimSpace(rawMultus) != "" {
		_ = json.Unmarshal([]byte(rawMultus), &existingMultus)
	}

	inUseSet := make(map[string]bool)

	// Merge secondary networks into Multus list and allocate dynamic IPAM if not specified
	for _, sec := range secondaryNetworks {
		foundIdx := -1
		for i, existing := range existingMultus {
			if existing.Name == sec.Name && (existing.Interface == sec.Interface || sec.Interface == "") {
				foundIdx = i
				break
			}
		}

		if foundIdx >= 0 {
			// If existing entry has no IP and client is available, allocate one
			if len(existingMultus[foundIdx].IPs) == 0 && c != nil {
				ipWithPrefix, err := allocateSecondaryIP(ctx, c, sec.Name, inUseSet)
				if err != nil {
					return false, fmt.Errorf("failed to allocate IP for secondary network %s: %w", sec.Name, err)
				}
				existingMultus[foundIdx].IPs = []string{ipWithPrefix}
			}
		} else {
			if len(sec.IPs) == 0 && c != nil {
				ipWithPrefix, err := allocateSecondaryIP(ctx, c, sec.Name, inUseSet)
				if err != nil {
					return false, fmt.Errorf("failed to allocate IP for secondary network %s: %w", sec.Name, err)
				}
				sec.IPs = []string{ipWithPrefix}
			}
			existingMultus = append(existingMultus, sec)
		}
	}

	multusBytes, err := json.Marshal(existingMultus)
	if err != nil {
		return false, err
	}
	pod.Annotations[AnnotationMultusNetworks] = string(multusBytes)

	// Sanitize networking.gke.io/interfaces so Cilium only sees the primary network
	if len(primaryIfaces) == 0 {
		primaryIfaces = append(primaryIfaces, GKEInterfaceSpec{
			InterfaceName: "eth0",
			Network:       "pod-network",
		})
	}
	primaryBytes, err := json.Marshal(primaryIfaces)
	if err != nil {
		return false, err
	}
	pod.Annotations[AnnotationGKEInterfaces] = string(primaryBytes)

	if _, hasDef := pod.Annotations[AnnotationGKEDefaultInterface]; !hasDef {
		pod.Annotations[AnnotationGKEDefaultInterface] = "eth0"
	}

	return true, nil
}

// allocateSecondaryIP discovers the subnet CIDR and gateway of a Network CR, builds an exclusion
// map of reserved infrastructure IPs and in-use Pod IPs, and returns the next free IP address with CIDR prefix.
func allocateSecondaryIP(ctx context.Context, c client.Client, netName string, inUseSet map[string]bool) (string, error) {
	netObj := &unstructured.Unstructured{}
	netObj.SetGroupVersionKind(NetworkGVK)
	if err := c.Get(ctx, client.ObjectKey{Name: netName}, netObj); err != nil {
		return "", fmt.Errorf("failed to fetch Network %s: %w", netName, err)
	}

	gateway4, _, _ := unstructured.NestedString(netObj.Object, "spec", "gateway4")
	prefixLen, found, _ := unstructured.NestedInt64(netObj.Object, "spec", "l2NetworkConfig", "prefixLength4")
	if !found || prefixLen <= 0 || prefixLen > 32 {
		prefixLen = DefaultPrefixLength
	}
	if gateway4 == "" {
		return "", fmt.Errorf("network %s has no gateway4 defined", netName)
	}

	gwIP := net.ParseIP(gateway4)
	if gwIP == nil || gwIP.To4() == nil {
		return "", fmt.Errorf("network %s has invalid gateway4: %s", netName, gateway4)
	}

	mask := net.CIDRMask(int(prefixLen), 32)
	netIP := gwIP.To4().Mask(mask)
	startUint := ipToUint32(netIP)
	maskUint := binary.BigEndian.Uint32(mask)
	broadcastUint := startUint | (^maskUint)

	excluded := make(map[string]bool)
	// Network address & broadcast address
	excluded[netIP.String()] = true
	excluded[uint32ToIP(broadcastUint).String()] = true
	// Gateway IP
	excluded[gateway4] = true

	// Reserve host node interface IPs (.2 through .9 relative to subnet start)
	for offset := uint32(2); offset <= 9 && (startUint+offset) < broadcastUint; offset++ {
		excluded[uint32ToIP(startUint+offset).String()] = true
	}

	// Exclude MetalLB VIP pool from Network annotations
	if rawVIPs, ok := netObj.GetAnnotations()[AnnotationLBServiceVIPs]; ok && rawVIPs != "" {
		var vipRanges []string
		if err := json.Unmarshal([]byte(rawVIPs), &vipRanges); err == nil {
			for _, r := range vipRanges {
				excludeIPRangeOrCIDR(excluded, r)
			}
		}
	}

	// Exclude GKEGatewayCIDR ranges for this network
	gwCIDRList := &unstructured.UnstructuredList{}
	gwCIDRList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.gke.io",
		Version: "v1",
		Kind:    "GKEGatewayCIDRList",
	})
	if err := c.List(ctx, gwCIDRList); err == nil {
		for _, item := range gwCIDRList.Items {
			netRef, _, _ := unstructured.NestedString(item.Object, "spec", "network")
			if netRef == netName {
				if ip4cidr, _, _ := unstructured.NestedString(item.Object, "spec", "ip4cidr"); ip4cidr != "" {
					excludeIPRangeOrCIDR(excluded, ip4cidr)
				}
			}
		}
	}

	// Collect active in-use secondary IPs across all pods
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList); err != nil {
		return "", fmt.Errorf("failed to list Pods for IPAM allocation: %w", err)
	}

	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed || !pod.DeletionTimestamp.IsZero() {
			continue
		}
		if rawMultus, ok := pod.Annotations[AnnotationMultusNetworks]; ok && rawMultus != "" {
			var multusList []MultusNetworkSpec
			if err := json.Unmarshal([]byte(rawMultus), &multusList); err == nil {
				for _, m := range multusList {
					if m.Name == netName {
						for _, ipStr := range m.IPs {
							cleanIP := strings.Split(strings.TrimSpace(ipStr), "/")[0]
							if cleanIP != "" {
								excluded[cleanIP] = true
							}
						}
					}
				}
			}
		}
		for _, statusKey := range []string{"k8s.v1.cni.cncf.io/network-status", "k8s.v1.cni.cncf.io/networks-status"} {
			if rawStatus, ok := pod.Annotations[statusKey]; ok && rawStatus != "" {
				var statusList []struct {
					Name string   `json:"name"`
					IPs  []string `json:"ips"`
				}
				if err := json.Unmarshal([]byte(rawStatus), &statusList); err == nil {
					for _, s := range statusList {
						if s.Name == netName || strings.HasSuffix(s.Name, "/"+netName) {
							for _, ipStr := range s.IPs {
								cleanIP := strings.Split(strings.TrimSpace(ipStr), "/")[0]
								if cleanIP != "" {
									excluded[cleanIP] = true
								}
							}
						}
					}
				}
			}
		}
	}

	for ip := range inUseSet {
		cleanIP := strings.Split(strings.TrimSpace(ip), "/")[0]
		excluded[cleanIP] = true
	}

	// Find the first available unallocated host IP
	for u := startUint + 1; u < broadcastUint; u++ {
		cand := uint32ToIP(u).String()
		if !excluded[cand] {
			inUseSet[cand] = true
			return fmt.Sprintf("%s/%d", cand, prefixLen), nil
		}
	}

	return "", fmt.Errorf("IP pool exhausted for secondary network %s (subnet %s/%d)", netName, netIP.String(), prefixLen)
}

func excludeIPRangeOrCIDR(excluded map[string]bool, val string) {
	val = strings.TrimSpace(val)
	if val == "" {
		return
	}
	if strings.Contains(val, "-") {
		parts := strings.Split(val, "-")
		if len(parts) == 2 {
			start := net.ParseIP(strings.TrimSpace(parts[0])).To4()
			end := net.ParseIP(strings.TrimSpace(parts[1])).To4()
			if start != nil && end != nil {
				startU := ipToUint32(start)
				endU := ipToUint32(end)
				if startU <= endU {
					for u := startU; u <= endU; u++ {
						excluded[uint32ToIP(u).String()] = true
					}
				}
			}
		}
		return
	}
	if strings.Contains(val, "/") {
		_, ipNet, err := net.ParseCIDR(val)
		if err == nil && ipNet.IP.To4() != nil {
			startU := ipToUint32(ipNet.IP.To4())
			maskU := binary.BigEndian.Uint32(ipNet.Mask)
			endU := startU | (^maskU)
			for u := startU; u <= endU; u++ {
				excluded[uint32ToIP(u).String()] = true
			}
		}
		return
	}
	if ip := net.ParseIP(val); ip != nil && ip.To4() != nil {
		excluded[ip.String()] = true
	}
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ip)
}

func uint32ToIP(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}
