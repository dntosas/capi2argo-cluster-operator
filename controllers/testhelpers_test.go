package controllers

import (
	b64 "encoding/base64"
	"log"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MockCapiKubeConfig returns a base64-encoded string that
// represents a valid KubeConfig definition.
func MockCapiKubeConfig() string {
	RawKubeConfig, err := os.ReadFile("../tests/capi-kubeconfig-eks.yaml")
	if err != nil {
		log.Fatal(err)
	}

	return b64.StdEncoding.EncodeToString(RawKubeConfig)
}

// MockCapiSecret returns a mock CAPI secret with configurable validity.
func MockCapiSecret(validMock bool, validType bool, validKey bool, name string, namespace string) *corev1.Secret {
	var v []byte
	if validMock {
		v, _ = b64.StdEncoding.DecodeString(MockCapiKubeConfig())
	} else {
		v = []byte("tester")
	}

	var t corev1.SecretType

	var vType corev1.SecretType = "cluster.x-k8s.io/secret"

	var iType corev1.SecretType = "tester/tester"

	if validType {
		t = vType
	} else {
		t = iType
	}

	var k string
	if validKey {
		k = "value"
	} else {
		k = "tester"
	}

	s := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"cluster.x-k8s.io/cluster-name": "test",
			},
		},
		Data: map[string][]byte{
			k: v,
		},
		Type: t,
	}

	return &s
}

// MockRancherSecret returns a mock secret of type Opaque (Rancher-style).
func MockRancherSecret(validMock bool, validKey bool, name string, namespace string) *corev1.Secret {
	var v []byte
	if validMock {
		v, _ = b64.StdEncoding.DecodeString(MockCapiKubeConfig())
	} else {
		v = []byte("tester")
	}

	var k string
	if validKey {
		k = "value"
	} else {
		k = "tester"
	}

	s := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"cluster.x-k8s.io/cluster-name": "test",
			},
		},
		Data: map[string][]byte{
			k: v,
		},
		Type: corev1.SecretTypeOpaque,
	}

	return &s
}

// MockArgoCluster returns a mock ArgoCluster with the given config.
func MockArgoCluster(validMock bool, cfg *Config) *ArgoCluster {
	var v string
	if validMock {
		v = b64.StdEncoding.EncodeToString([]byte("tester"))
	} else {
		v = "tester"
	}

	a := &ArgoCluster{
		NamespacedName: BuildNamespacedName("test", "test", cfg),
		ClusterName:    "test",
		ClusterServer:  "server",
		ClusterLabels: map[string]string{
			"capi-to-argocd/cluster-secret-name": "test-kubeconfig",
			"capi-to-argocd/cluster-namespace":   "test",
		},
		ClusterConfig: ArgoConfig{
			BearerToken: &v,
			TLSClientConfig: &ArgoTLS{
				CaData:   &v,
				CertData: &v,
				KeyData:  &v,
			},
		},
	}

	return a
}

// MockArgoSecret returns a mock ArgoCD secret.
func MockArgoSecret(cfg *Config) *corev1.Secret {
	a := MockArgoCluster(true, cfg)
	s, _ := a.ConvertToSecret()

	return s
}
