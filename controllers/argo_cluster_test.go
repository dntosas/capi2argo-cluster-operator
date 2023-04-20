package controllers

import (
	b64 "encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

func TestConvertToSecret(t *testing.T) {
	t.Parallel()
	validMock := true
	tests := []struct {
		testName           string
		testMock           *ArgoCluster
		testExpectedError  bool
		testExpectedValues map[string]string
	}{
		{"test type with valid fields", MockArgoCluster(validMock), false,
			map[string]string{
				"Kind":            "Secret",
				"APIVersion":      "v1",
				"Name":            "cluster-test",
				"Namespace":       ArgoNamespace,
				"OperatorLabel":   GetArgoCommonLabels()["capi-to-argocd/owned"],
				"ArgoLabel":       GetArgoCommonLabels()["argocd.argoproj.io/secret-type"],
				"SecretNameLabel": "test-kubeconfig",
				"NamespaceLabel":  "test",
			},
		},
		{"test type with non-valid fields", MockArgoCluster(!validMock), true, nil},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()
			s, err := tt.testMock.ConvertToSecret()
			if !tt.testExpectedError {
				assert.NotNil(t, s)
				assert.Nil(t, err)
				if tt.testExpectedValues != nil {
					assert.Equal(t, tt.testExpectedValues["Kind"], s.TypeMeta.Kind)
					assert.Equal(t, tt.testExpectedValues["APIVersion"], s.TypeMeta.APIVersion)
					assert.Equal(t, tt.testExpectedValues["Name"], s.ObjectMeta.Name)
					assert.Equal(t, tt.testExpectedValues["Namespace"], s.ObjectMeta.Namespace)
					assert.Equal(t, tt.testExpectedValues["OperatorLabel"], s.ObjectMeta.Labels["capi-to-argocd/owned"])
					assert.Equal(t, tt.testExpectedValues["ArgoLabel"], s.ObjectMeta.Labels["argocd.argoproj.io/secret-type"])
					assert.Equal(t, tt.testExpectedValues["SecretNameLabel"], s.ObjectMeta.Labels["capi-to-argocd/cluster-secret-name"])
					assert.Equal(t, tt.testExpectedValues["NamespaceLabel"], s.ObjectMeta.Labels["capi-to-argocd/cluster-namespace"])
					_, err = yaml.Marshal(s)
					assert.Nil(t, err)
				}
			} else {
				assert.Nil(t, s)
				assert.NotNil(t, err)
			}
		})
	}
}

func TestValidateClusterTLSConfig(t *testing.T) {
	// Create a dummy valid b64 string
	enc := b64.StdEncoding.EncodeToString([]byte("test"))

	t.Parallel()
	tests := []struct {
		testName          string
		testMock          *ArgoTLS
		testExpectedError bool
	}{
		{"test type with valid fields", &ArgoTLS{CaData: enc, CertData: enc, KeyData: enc}, false},
		{"test type with non-valid field", &ArgoTLS{CaData: "non-valid", CertData: enc, KeyData: enc}, true},
		{"test type with missing fields", &ArgoTLS{CaData: enc}, true},
		{"test empty type", &ArgoTLS{}, true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()
			err := ValidateClusterTLSConfig(tt.testMock)
			if !tt.testExpectedError {
				assert.Nil(t, err)
			} else {
				assert.NotNil(t, err)
			}
		})
	}
}

func TestBuildNamespacedName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		testName                  string
		testMock                  string
		testNamespace             string
		testEnableNamespacedNames bool
		testExpectedError         bool
		testExpectedValues        types.NamespacedName
	}{
		{"test type with valid fields", "test-XXX-kubeconfig", "test-ns", false, false,
			types.NamespacedName{
				Name:      "cluster-test-XXX",
				Namespace: ArgoNamespace,
			},
		},
		{"test type with valid fields and namespaced names", "test-XXX-kubeconfig", "test-ns", true, false,
			types.NamespacedName{
				Name:      "cluster-test-ns-test-XXX",
				Namespace: ArgoNamespace,
			},
		},
		{"test type with non-valid fields", "capi-XXX", "test-ns", false, false,
			types.NamespacedName{
				Name:      "cluster-capi-XXX",
				Namespace: ArgoNamespace,
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.testName, func(t *testing.T) {
			oldConf := EnableNamespacedNames
			EnableNamespacedNames = tt.testEnableNamespacedNames
			s := BuildNamespacedName(tt.testMock, tt.testNamespace)
			EnableNamespacedNames = oldConf
			if !tt.testExpectedError {
				assert.NotNil(t, s)
				assert.Equal(t, tt.testExpectedValues.Name, s.Name)
				assert.Equal(t, tt.testExpectedValues.Namespace, s.Namespace)
			} else {
				assert.Nil(t, s)
			}
		})
	}
}
