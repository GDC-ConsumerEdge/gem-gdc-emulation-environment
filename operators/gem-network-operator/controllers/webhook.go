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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	MutatingWebhookConfigName = "gem-pod-interface-mutator"
	WebhookPath               = "/mutate-pod"
)

// GenerateWebhookCerts generates a self-signed CA and server certificate valid for the given host,
// writes tls.crt and tls.key into certDir, and returns the CA certificate PEM bytes for registration.
func GenerateWebhookCerts(certDir, host string) ([]byte, error) {
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cert directory: %w", err)
	}

	// 1. Generate CA Key and Cert
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA key: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "gem-network-operator-ca",
			Organization: []string{"Google LLC"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	caDer, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDer})

	// 2. Generate Server Key and Cert signed by CA
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{"Google LLC"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add SANs
	serverTemplate.DNSNames = []string{"localhost", host}
	if ip := net.ParseIP(host); ip != nil {
		serverTemplate.IPAddresses = []net.IP{ip, net.ParseIP("127.0.0.1")}
	} else {
		serverTemplate.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}

	serverDer, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create server certificate: %w", err)
	}

	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDer})

	keyBytes, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server key: %w", err)
	}
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	// Write tls.crt and tls.key
	certPath := filepath.Join(certDir, "tls.crt")
	keyPath := filepath.Join(certDir, "tls.key")

	if err := os.WriteFile(certPath, serverCertPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to write tls.crt: %w", err)
	}
	if err := os.WriteFile(keyPath, serverKeyPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to write tls.key: %w", err)
	}

	return caPEM, nil
}

// EnsureMutatingWebhookConfiguration creates or updates the MutatingWebhookConfiguration in the cluster.
func EnsureMutatingWebhookConfiguration(ctx context.Context, k8sClient client.Client, webhookURL string, caBundle []byte) error {
	failurePolicy := admissionregistrationv1.Ignore
	matchPolicy := admissionregistrationv1.Equivalent
	sideEffects := admissionregistrationv1.SideEffectClassNone
	scope := admissionregistrationv1.NamespacedScope
	timeoutSeconds := int32(5)

	mwc := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: MutatingWebhookConfigName,
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name: "pod-interface-mutator.gem.networking.gke.io",
				AdmissionReviewVersions: []string{
					"v1",
				},
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					URL:      &webhookURL,
					CABundle: caBundle,
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
							Scope:       &scope,
						},
					},
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "kubernetes.io/metadata.name",
							Operator: metav1.LabelSelectorOpNotIn,
							Values: []string{
								"kube-system",
								"gatekeeper-system",
								"gke-system",
							},
						},
					},
				},
				FailurePolicy:  &failurePolicy,
				MatchPolicy:    &matchPolicy,
				SideEffects:    &sideEffects,
				TimeoutSeconds: &timeoutSeconds,
			},
		},
	}

	existing := &admissionregistrationv1.MutatingWebhookConfiguration{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: MutatingWebhookConfigName}, existing)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		return k8sClient.Create(ctx, mwc)
	}

	// Update existing configuration
	existing.Webhooks = mwc.Webhooks
	return k8sClient.Update(ctx, existing)
}
