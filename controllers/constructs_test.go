package controllers

import (
	b64 "encoding/base64"
	"log"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MockCapiKubeConfig returns a based64-encoded string that
// represents a valid KubeConfig definition.
func MockCapiKubeConfig() string {
	RawKubeConfig, err := os.ReadFile("../tests/capi-kubeconfig-eks.yaml")
	if err != nil {
		log.Fatal(err)
	}

	return b64.StdEncoding.EncodeToString(RawKubeConfig)
}

func MockCapiSecret(validMock bool, validType bool, validKey bool, name string, namespace string) *corev1.Secret {
	// If validMock=true, return type with proper b64 encoded values
	var v []byte
	if validMock {
		v, _ = b64.StdEncoding.DecodeString(MockCapiKubeConfig())
	} else {
		v = []byte("tester")
	}

	// If validType=true, return type with proper .type
	var t corev1.SecretType
	var vType corev1.SecretType = "cluster.x-k8s.io/secret"
	var iType corev1.SecretType = "tester/tester"
	if validType {
		t = vType
	} else {
		t = iType
	}

	// If validKey=true, return type with proper data.key
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

func MockArgoCluster(validMock bool) *ArgoCluster {
	// If validMock=true, return type with proper b64 encoded values
	var v string
	if validMock {
		v = b64.StdEncoding.EncodeToString([]byte("tester"))
	} else {
		v = "tester"
	}

	a := &ArgoCluster{
		NamespacedName: BuildNamespacedName("test", "test"),
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

func MockArgoSecret() *corev1.Secret {
	a := MockArgoCluster(true)
	s, _ := a.ConvertToSecret()
	return s
}

// IsBase64 returns true if given value is valid b64-encoded stream
func IsBase64(s string) bool {
	_, err := b64.StdEncoding.DecodeString(s)
	return err == nil
}
