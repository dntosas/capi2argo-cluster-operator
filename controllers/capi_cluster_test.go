package controllers

import (
	b64 "encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

var (
	defaultValidMock = true
	defaultValidType = true
	defaultValidKey  = true
	defaultName      = "test"
	defaultNamespace = "test"
)

func TestUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName           string
		testMock           *corev1.Secret
		testExpectedError  error
		testExpectedValues map[string]string
	}{
		{"test type with valid fields", MockCapiSecret(defaultValidMock, defaultValidType, defaultValidKey, defaultName, defaultNamespace), nil,
			map[string]string{
				"Kind":        "Config",
				"APIVersion":  "v1",
				"ClusterName": "kube-cluster-test",
				"UserName":    "kube-cluster-test-admin",
				"CaData":      "",
				"KeyData":     "dGVzdGVyCg==",
				"Server":      "https://kube-cluster-test.domain.com:6443",
				"Token":       "e",
			},
		},
		{"test type with wrong secret.Data[key]", MockCapiSecret(defaultValidMock, defaultValidType, !defaultValidKey, defaultName, defaultNamespace), ErrWrongSecretKey, nil},
		{"test type with wrong secret.Type", MockCapiSecret(defaultValidMock, !defaultValidType, defaultValidKey, defaultName, defaultNamespace), ErrWrongSecretType, nil},
		{"test Rancher secret (Opaque type) with valid fields", MockRancherSecret(defaultValidMock, defaultValidKey, defaultName, defaultNamespace), nil,
			map[string]string{
				"Kind":        "Config",
				"APIVersion":  "v1",
				"ClusterName": "kube-cluster-test",
				"UserName":    "kube-cluster-test-admin",
				"CaData":      "",
				"KeyData":     "dGVzdGVyCg==",
				"Server":      "https://kube-cluster-test.domain.com:6443",
				"Token":       "e",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			c := NewCapiCluster(defaultName, defaultNamespace)
			err := c.Unmarshal(tt.testMock)

			if tt.testExpectedError != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.testExpectedError)
			} else {
				assert.NotNil(t, c)
				assert.NoError(t, err)

				if tt.testExpectedValues != nil {
					assert.Equal(t, tt.testExpectedValues["Kind"], c.KubeConfig.Kind)
					assert.Equal(t, tt.testExpectedValues["APIVersion"], c.KubeConfig.APIVersion)
					assert.Equal(t, tt.testExpectedValues["ClusterName"], c.KubeConfig.Clusters[0].Name)
					assert.Equal(t, tt.testExpectedValues["Server"], c.KubeConfig.Clusters[0].Cluster.Server)
					assert.Equal(t, tt.testExpectedValues["UserName"], c.KubeConfig.Users[0].Name)

					if c.KubeConfig.Users[0].User.CertData != nil {
						assert.Eventually(t, func() bool {
							_, err := b64.StdEncoding.DecodeString(*c.KubeConfig.Users[0].User.CertData)
							return err == nil
						}, time.Second, 100*time.Millisecond)
					}

					if c.KubeConfig.Users[0].User.KeyData != nil {
						assert.Eventually(t, func() bool {
							_, err := b64.StdEncoding.DecodeString(*c.KubeConfig.Users[0].User.KeyData)
							return err == nil
						}, time.Second, 100*time.Millisecond)
					}

					if c.KubeConfig.Users[0].User.Token != nil {
						assert.Eventually(t, func() bool {
							_, err := b64.StdEncoding.DecodeString(*c.KubeConfig.Users[0].User.Token)
							return err == nil
						}, time.Second, 100*time.Millisecond)
					}

					assert.Eventually(t, func() bool {
						_, err := b64.StdEncoding.DecodeString(c.KubeConfig.Clusters[0].Cluster.CaData)
						return err == nil
					}, time.Second, 100*time.Millisecond)

					assert.GreaterOrEqual(t, len(c.KubeConfig.Clusters), 1)
					assert.GreaterOrEqual(t, len(c.KubeConfig.Users), 1)
					_, err = yaml.Marshal(c)
					assert.Nil(t, err)
				}
			}
		})
	}
}

func TestNewCapiCluster(t *testing.T) {
	t.Parallel()

	c := NewCapiCluster("test", "test")
	assert.IsType(t, &CapiCluster{}, c)
}

func TestValidateCapiSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName    string
		testMock    *corev1.Secret
		expectedErr error
	}{
		{"test type with valid fields", MockCapiSecret(defaultValidMock, defaultValidType, defaultValidKey, defaultName, defaultNamespace), nil},
		{"test type with wrong secret.Data[key]", MockCapiSecret(defaultValidMock, defaultValidType, !defaultValidKey, defaultName, defaultNamespace), ErrWrongSecretKey},
		{"test type with wrong secret.Type", MockCapiSecret(defaultValidMock, !defaultValidType, defaultValidKey, defaultName, defaultNamespace), ErrWrongSecretType},
		{"test Rancher secret (Opaque type) with valid fields", MockRancherSecret(defaultValidMock, defaultValidKey, defaultName, defaultNamespace), nil},
		{"test Rancher secret (Opaque type) with wrong secret.Data[key]", MockRancherSecret(defaultValidMock, !defaultValidKey, defaultName, defaultNamespace), ErrWrongSecretKey},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			err := ValidateCapiSecret(tt.testMock)
			if tt.expectedErr != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCapiNaming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid kubeconfig suffix", "my-cluster-kubeconfig", true},
		{"user kubeconfig should be rejected", "my-cluster-user-kubeconfig", false},
		{"no kubeconfig suffix", "my-cluster-secret", false},
		{"empty name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			nn := types.NamespacedName{Name: tt.input, Namespace: "default"}
			assert.Equal(t, tt.expected, ValidateCapiNaming(nn))
		})
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	t.Parallel()

	assert.False(t, errors.Is(ErrWrongSecretType, ErrWrongSecretKey))
	assert.False(t, errors.Is(ErrWrongSecretType, ErrInvalidKubeConfig))
	assert.False(t, errors.Is(ErrWrongSecretKey, ErrInvalidKubeConfig))
}
