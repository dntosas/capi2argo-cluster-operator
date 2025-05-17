package controllers

import (
	// b64 "encoding/base64".
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

			v, errors := buildTakeAlongLabels(tt.testMock)
			if tt.testExpectedError {
				assert.NotEmpty(t, errors)
			} else {
				assert.Empty(t, errors)
			}

			assert.Equal(t, v, tt.testExpectedValues)
		})
	}
}

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
		// {"test type with non-valid fields", MockArgoCluster(!validMock), true, nil},
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

// func TestValidateClusterTLSConfig(t *testing.T) {
// 	// Create a dummy valid b64 string
// 	enc := b64.StdEncoding.EncodeToString([]byte("test"))
// 	nonValid := "non-valid"

// 	t.Parallel()
// 	tests := []struct {
// 		testName          string
// 		testMock          *ArgoTLS
// 		testExpectedError bool
// 	}{
// 		{"test type with valid fields", &ArgoTLS{CaData: &enc, CertData: &enc, KeyData: &enc}, false},
// 		{"test type with non-valid field", &ArgoTLS{CaData: &nonValid, CertData: &enc, KeyData: &enc}, true},
// 		{"test type with missing fields", &ArgoTLS{CaData: &enc}, true},
// 		{"test empty type", &ArgoTLS{}, true},
// 	}
// 	for _, tt := range tests {
// 		tt := tt
// 		t.Run(tt.testName, func(t *testing.T) {
// 			t.Parallel()
// 			err := ValidateClusterTLSConfig(tt.testMock)
// 			if !tt.testExpectedError {
// 				assert.Nil(t, err)
// 			} else {
// 				assert.NotNil(t, err)
// 			}
// 		})
// 	}
// }

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
