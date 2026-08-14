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
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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
	Name      string `json:"name"`
	Interface string `json:"interface,omitempty"`
}

// PodInterfaceMutator mutates Pod definitions carrying GDC Connected networking.gke.io/interfaces
// annotations into Multus k8s.v1.cni.cncf.io/networks annotations, allowing unmodified GDC manifests
// to attach secondary networks seamlessly in GEM.
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

	mutated := MutatePodInterfaces(pod)
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

// MutatePodInterfaces inspects a Pod's networking.gke.io/interfaces annotation. If secondary
// network entries are found, it injects matching k8s.v1.cni.cncf.io/networks entries for Multus
// and sanitizes networking.gke.io/interfaces so Cilium only configures the primary network interface.
// It returns true if any mutations were performed.
func MutatePodInterfaces(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}

	rawGKEInterfaces, ok := pod.Annotations[AnnotationGKEInterfaces]
	if !ok || strings.TrimSpace(rawGKEInterfaces) == "" {
		return false
	}

	var gkeIfaces []GKEInterfaceSpec
	if err := json.Unmarshal([]byte(rawGKEInterfaces), &gkeIfaces); err != nil {
		return false
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
		return false
	}

	// Parse existing Multus networks if already specified
	var existingMultus []MultusNetworkSpec
	if rawMultus, hasMultus := pod.Annotations[AnnotationMultusNetworks]; hasMultus && strings.TrimSpace(rawMultus) != "" {
		_ = json.Unmarshal([]byte(rawMultus), &existingMultus)
	}

	// Merge secondary networks into Multus list
	for _, sec := range secondaryNetworks {
		exists := false
		for _, existing := range existingMultus {
			if existing.Name == sec.Name && (existing.Interface == sec.Interface || sec.Interface == "") {
				exists = true
				break
			}
		}
		if !exists {
			existingMultus = append(existingMultus, sec)
		}
	}

	multusBytes, err := json.Marshal(existingMultus)
	if err != nil {
		return false
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
		return false
	}
	pod.Annotations[AnnotationGKEInterfaces] = string(primaryBytes)

	if _, hasDef := pod.Annotations[AnnotationGKEDefaultInterface]; !hasDef {
		pod.Annotations[AnnotationGKEDefaultInterface] = "eth0"
	}

	return true
}
