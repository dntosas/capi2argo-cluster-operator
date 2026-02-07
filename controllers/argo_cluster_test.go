package controllers

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/yaml"
)

func TestExtractTakeAlongLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName           string
		testMock           string
		testExpectedError  bool
		testExpectedValues string
	}{
		{"Test with valid take-along-labels label", fmt.Sprintf("%s%s", clusterTakeAlongKey, "foo"), false, "foo"},
		{"Test with complex and valid take-along-labels label", fmt.Sprintf("%s%s", clusterTakeAlongKey, "my.mydomain.com/subkey"), false, "my.mydomain.com/subkey"},
		{"Test with no take-along-labels labels", clusterTakeAlongKey, true, ""},
		{"Test with standard label", "mylabel", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			v, err := extractTakeAlongLabel(tt.testMock)
			if tt.testExpectedError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, v, tt.testExpectedValues)
			}
		})
	}
}

func TestBuildTakeAlongLabels(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		ArgoNamespace:       "argocd",
		EnableAutoLabelCopy: false,
	}

	tests := []struct {
		testName           string
		testMock           *clusterv1.Cluster
		testExpectedError  bool
		testExpectedValues map[string]string
	}{
		{"Test with no take-along-labels label",
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			}, false, map[string]string{}},
		{"Test with take-along-labels label (multiple)",
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"test":                   "dont-take-along",
						"foo":                    "bar",
						"my.mydomain.com/subkey": "foo",
						fmt.Sprintf("%s%s", clusterTakeAlongKey, "foo"):                    "",
						fmt.Sprintf("%s%s", clusterTakeAlongKey, "my.mydomain.com/subkey"): "",
					},
				},
			}, false, map[string]string{
				"foo":                    "bar",
				"my.mydomain.com/subkey": "foo",
				fmt.Sprintf("%s%s", clusterTakenFromClusterKey, "foo"):                    "",
				fmt.Sprintf("%s%s", clusterTakenFromClusterKey, "my.mydomain.com/subkey"): "",
			}},
		{"Test with take-along-labels label (single)",
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"test": "dont-take-along",
						"bar":  "foo",
						"foo":  "bar",
						fmt.Sprintf("%s%s", clusterTakeAlongKey, "foo"): "",
					},
				},
			}, false, map[string]string{
				"foo": "bar",
				fmt.Sprintf("%s%s", clusterTakenFromClusterKey, "foo"): "",
			}},
		{"Test with take-along-labels label (single) and take-along-labels label not found",
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"test": "dont-take-along",
						"bar":  "foo",
						"foo":  "bar",
						fmt.Sprintf("%s%s", clusterTakeAlongKey, "invalid"): "",
					},
				},
			}, true, map[string]string{}},
		{"Test with take-along-labels label (multiple) and take-along-labels label not found",
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"test":                   "dont-take-along",
						"bar":                    "foo",
						"my.mydomain.com/subkey": "bar",
						fmt.Sprintf("%s%s", clusterTakeAlongKey, "my.mydomain.com/subkey"): "",
						fmt.Sprintf("%s%s", clusterTakeAlongKey, "invalid"):                "",
					},
				},
			}, true, map[string]string{
				"my.mydomain.com/subkey": "bar",
				fmt.Sprintf("%s%s", clusterTakenFromClusterKey, "my.mydomain.com/subkey"): "",
			}},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			v, errs := buildTakeAlongLabels(tt.testMock, cfg)
			if tt.testExpectedError {
				assert.NotEmpty(t, errs)
			} else {
				assert.Empty(t, errs)
			}

			assert.Equal(t, v, tt.testExpectedValues)
		})
	}
}

func TestConvertToSecret(t *testing.T) {
	t.Parallel()

	cfg := &Config{ArgoNamespace: "argocd"}

	tests := []struct {
		testName           string
		testMock           *ArgoCluster
		testExpectedError  bool
		testExpectedValues map[string]string
	}{
		{"test type with valid fields", MockArgoCluster(true, cfg), false,
			map[string]string{
				"Kind":            "Secret",
				"APIVersion":      "v1",
				"Name":            "cluster-test",
				"Namespace":       cfg.ArgoNamespace,
				"OperatorLabel":   GetArgoCommonLabels()["capi-to-argocd/owned"],
				"ArgoLabel":       GetArgoCommonLabels()["argocd.argoproj.io/secret-type"],
				"SecretNameLabel": "test-kubeconfig",
				"NamespaceLabel":  "test",
			},
		},
	}
	for _, tt := range tests {
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

func TestBuildAutoLabelCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName           string
		testMock           map[string]string
		testExpectedValues map[string]string
	}{
		{
			"Test with user labels only",
			map[string]string{
				"foo":                       "bar",
				"my.domain.com/environment": "prod",
			},
			map[string]string{
				"foo":                       "bar",
				"my.domain.com/environment": "prod",
			},
		},
		{
			"Test with mixed labels (user + system)",
			map[string]string{
				"foo":                                  "bar",
				"kubernetes.io/cluster-name":           "test",
				"cluster.x-k8s.io/cluster-name":       "test",
				"capi-to-argocd/owned":                 "true",
				"take-along-label.capi-to-argocd.foo":  "",
				"ignore-cluster.capi-to-argocd":        "",
				"my.domain.com/environment":            "prod",
			},
			map[string]string{
				"foo":                       "bar",
				"my.domain.com/environment": "prod",
			},
		},
		{
			"Test with only system labels",
			map[string]string{
				"kubernetes.io/cluster-name":     "test",
				"cluster.x-k8s.io/cluster-name": "test",
			},
			map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			result := buildAutoLabelCopy(tt.testMock)
			assert.Equal(t, tt.testExpectedValues, result)
		})
	}
}

func TestBuildTakeAlongLabelsWithAutoMode(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		ArgoNamespace:       "argocd",
		EnableAutoLabelCopy: true,
	}

	tests := []struct {
		testName           string
		testMock           *clusterv1.Cluster
		testExpectedValues map[string]string
	}{
		{
			"Test auto mode with user labels",
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"foo":                            "bar",
						"my.domain.com/environment":      "prod",
						"cluster.x-k8s.io/cluster-name": "test",
					},
				},
			},
			map[string]string{
				"foo":                       "bar",
				"my.domain.com/environment": "prod",
			},
		},
		{
			"Test auto mode filters out system labels",
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"user-label":                     "value",
						"kubernetes.io/cluster-name":     "test",
						"cluster.x-k8s.io/cluster-name": "test",
						"capi-to-argocd/owned":           "true",
					},
				},
			},
			map[string]string{
				"user-label": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			result, errs := buildTakeAlongLabels(tt.testMock, cfg)
			assert.Empty(t, errs)
			assert.Equal(t, tt.testExpectedValues, result)
		})
	}
}

func TestBuildNamespacedName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName           string
		testMock           string
		testNamespace      string
		testConfig         *Config
		testExpectedValues types.NamespacedName
	}{
		{"test type with valid fields", "test-XXX-kubeconfig", "test-ns",
			&Config{ArgoNamespace: "argocd", EnableNamespacedNames: false},
			types.NamespacedName{
				Name:      "cluster-test-XXX",
				Namespace: "argocd",
			},
		},
		{"test type with valid fields and namespaced names", "test-XXX-kubeconfig", "test-ns",
			&Config{ArgoNamespace: "argocd", EnableNamespacedNames: true},
			types.NamespacedName{
				Name:      "cluster-test-ns-test-XXX",
				Namespace: "argocd",
			},
		},
		{"test type with non-valid fields", "capi-XXX", "test-ns",
			&Config{ArgoNamespace: "argocd", EnableNamespacedNames: false},
			types.NamespacedName{
				Name:      "cluster-capi-XXX",
				Namespace: "argocd",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			s := BuildNamespacedName(tt.testMock, tt.testNamespace, tt.testConfig)
			assert.Equal(t, tt.testExpectedValues.Name, s.Name)
			assert.Equal(t, tt.testExpectedValues.Namespace, s.Namespace)
		})
	}
}

func TestValidateClusterIgnoreLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName          string
		testMock          *clusterv1.Cluster
		testExpectedValue bool
	}{
		{
			"Test with nil cluster",
			nil,
			false,
		},
		{
			"Test with cluster without ignore label",
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
			false,
		},
		{
			"Test with cluster with ignore label",
			&clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						"foo":                           "bar",
						"ignore-cluster.capi-to-argocd": "",
					},
				},
			},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			result := validateClusterIgnoreLabel(tt.testMock)
			assert.Equal(t, tt.testExpectedValue, result)
		})
	}
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	t.Run("ErrWrongSecretType is distinguishable", func(t *testing.T) {
		t.Parallel()
		wrapped := fmt.Errorf("context: %w", ErrWrongSecretType)
		assert.True(t, errors.Is(wrapped, ErrWrongSecretType))
		assert.False(t, errors.Is(wrapped, ErrWrongSecretKey))
	})

	t.Run("ErrWrongSecretKey is distinguishable", func(t *testing.T) {
		t.Parallel()
		wrapped := fmt.Errorf("context: %w", ErrWrongSecretKey)
		assert.True(t, errors.Is(wrapped, ErrWrongSecretKey))
		assert.False(t, errors.Is(wrapped, ErrWrongSecretType))
	})

	t.Run("ErrInvalidKubeConfig is distinguishable", func(t *testing.T) {
		t.Parallel()
		wrapped := fmt.Errorf("context: %w", ErrInvalidKubeConfig)
		assert.True(t, errors.Is(wrapped, ErrInvalidKubeConfig))
		assert.False(t, errors.Is(wrapped, ErrWrongSecretType))
	})
}
