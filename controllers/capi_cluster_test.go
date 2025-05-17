package controllers

import (
	b64 "encoding/base64"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
	"testing"
	"time"
)

var (
	validMock = true
	validType = true
	validKey  = true
	name      = "test"
	namespace = "test"
)

func TestUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName           string
		testMock           *corev1.Secret
		testExpectedError  bool
		testExpectedValues map[string]string
	}{
		{"test type with valid fields", MockCapiSecret(validMock, validType, validKey, name, namespace), false,
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
		{"test type with wrong secret.Data[key]", MockCapiSecret(validMock, validType, !validKey, name, namespace), true,
			map[string]string{
				"ErrorMsg": "wrong secret key",
			},
		},
		{"test type with wrong secret.Type", MockCapiSecret(validMock, !validType, validKey, name, namespace), true,
			map[string]string{
				"ErrorMsg": "wrong secret type",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			c := NewCapiCluster(name, namespace)
			err := c.Unmarshal(tt.testMock)

			if !tt.testExpectedError {
				assert.NotNil(t, c)
				assert.Nil(t, err)

				if tt.testExpectedValues != nil {
					// Check expected values.
					assert.Equal(t, tt.testExpectedValues["Kind"], c.KubeConfig.Kind)
					assert.Equal(t, tt.testExpectedValues["APIVersion"], c.KubeConfig.APIVersion)
					assert.Equal(t, tt.testExpectedValues["ClusterName"], c.KubeConfig.Clusters[0].Name)
					assert.Equal(t, tt.testExpectedValues["Server"], c.KubeConfig.Clusters[0].Cluster.Server)
					assert.Equal(t, tt.testExpectedValues["UserName"], c.KubeConfig.Users[0].Name)
					// Check that we get proper binary values for specific fields.
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
					// Get at least one cluster/user per secret.
					assert.GreaterOrEqual(t, len(c.KubeConfig.Clusters), 1)
					assert.GreaterOrEqual(t, len(c.KubeConfig.Users), 1)
					_, err = yaml.Marshal(c)
					assert.Nil(t, err)
				}
			} else {
				assert.NotNil(t, err)

				if assert.Error(t, err) {
					assert.Equal(t, tt.testExpectedValues["ErrorMsg"], err.Error())
				}
			}
		})
	}
}

func TestNewCapiCluster(t *testing.T) {
	c := NewCapiCluster("test", "test")
	assert.IsType(t, &CapiCluster{}, c)
}

func TestValidateCapiSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName           string
		testMock           *corev1.Secret
		testExpectedError  bool
		testExpectedValues map[string]string
	}{
		{"test type with valid fields", MockCapiSecret(validMock, validType, validKey, name, namespace), false, nil},
		{"test type with wrong secret.Data[key]", MockCapiSecret(validMock, validType, !validKey, name, namespace), true,
			map[string]string{
				"ErrorMsg": "wrong secret key",
			},
		},
		{"test type with wrong secret.Type", MockCapiSecret(validMock, !validType, validKey, name, namespace), true,
			map[string]string{
				"ErrorMsg": "wrong secret type",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			err := ValidateCapiSecret(tt.testMock)
			if !tt.testExpectedError {
				assert.Nil(t, err)
			} else {
				assert.NotNil(t, err)

				if tt.testExpectedValues != nil {
					if assert.Error(t, err) {
						assert.Equal(t, tt.testExpectedValues["ErrorMsg"], err.Error())
					}
				}
			}
		})
	}
}
