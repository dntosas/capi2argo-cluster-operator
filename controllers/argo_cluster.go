// Package controllers implements functions for manipulating CAPI generated
// cluster secrets into Argo definitions.
package controllers

import (
	// b64 "encoding/base64".
	"encoding/json"
	// "errors".
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	// ArgoNamespace represents the Namespace that hold ArgoCluster secrets.
	ArgoNamespace string
	// TestKubeConfig represents.
	TestKubeConfig *rest.Config
)

const (
	clusterTakeAlongKey        = "take-along-label.capi-to-argocd."
	clusterTakenFromClusterKey = "taken-from-cluster-label.capi-to-argocd."
	clusterIgnoreKey           = "ignore-cluster.capi-to-argocd"
)

// GetArgoCommonLabels holds a map of labels that reconciled objects must have.
func GetArgoCommonLabels() map[string]string {
	return map[string]string{
		"capi-to-argocd/owned":           "true",
		"argocd.argoproj.io/secret-type": "cluster",
	}
}

// ArgoCluster holds all information needed for CAPI --> Argo Cluster conversion.
type ArgoCluster struct {
	NamespacedName  types.NamespacedName
	ClusterName     string
	ClusterServer   string
	ClusterLabels   map[string]string
	TakeAlongLabels map[string]string
	ClusterConfig   ArgoConfig
}

// ArgoConfig represents Argo Cluster.JSON.config.
type ArgoConfig struct {
	TLSClientConfig *ArgoTLS `json:"tlsClientConfig,omitempty"`
	BearerToken     *string  `json:"bearerToken,omitempty"`
}

// ArgoTLS represents Argo Cluster.JSON.config.tlsClientConfig.
type ArgoTLS struct {
	CaData   *string `json:"caData,omitempty"`
	CertData *string `json:"certData,omitempty"`
	KeyData  *string `json:"keyData,omitempty"`
}

// NewArgoCluster return a new ArgoCluster.
func NewArgoCluster(c *CapiCluster, s *corev1.Secret, cluster *clusterv1.Cluster) (*ArgoCluster, error) {
	log := ctrl.Log.WithName("argoCluster")

	takeAlongLabels := map[string]string{}

	var errList []string

	if cluster != nil {
		takeAlongLabels, errList = buildTakeAlongLabels(cluster)
		for _, e := range errList {
			log.Info(e)
		}
	}

	return &ArgoCluster{
		NamespacedName: BuildNamespacedName(s.ObjectMeta.Name, s.ObjectMeta.Namespace),
		ClusterName:    BuildClusterName(c.KubeConfig.Clusters[0].Name, s.ObjectMeta.Namespace),
		ClusterServer:  c.KubeConfig.Clusters[0].Cluster.Server,
		ClusterLabels: map[string]string{
			"capi-to-argocd/cluster-secret-name": c.Name + "-kubeconfig",
			"capi-to-argocd/cluster-namespace":   c.Namespace,
		},
		TakeAlongLabels: takeAlongLabels,
		ClusterConfig: ArgoConfig{
			BearerToken: c.KubeConfig.Users[0].User.Token,
			TLSClientConfig: &ArgoTLS{
				CaData:   &c.KubeConfig.Clusters[0].Cluster.CaData,
				CertData: c.KubeConfig.Users[0].User.CertData,
				KeyData:  c.KubeConfig.Users[0].User.KeyData,
			},
		},
	}, nil
}

// extractTakeAlongLabel returns the take-along label key from a cluster resource.
func extractTakeAlongLabel(key string) (string, error) {
	if strings.HasPrefix(key, clusterTakeAlongKey) {
		splitResult := strings.Split(key, clusterTakeAlongKey)
		if len(splitResult) >= 2 {
			if splitResult[1] != "" {
				return splitResult[1], nil
			}
		}

		return "", fmt.Errorf("invalid take-along label. missing key after '/': %s", key)
	}
	// Not an take-along label. Return nil
	return "", nil
}

// validateClusterIgnoreLabel returns true when the cluster has the clusterIgnoreKey label.
func validateClusterIgnoreLabel(cluster *clusterv1.Cluster) bool {
	clusterLabels := cluster.Labels

	for k := range clusterLabels {
		if k == clusterIgnoreKey {
			return true
		}
	}

	return false
}

// buildAutoLabelCopy copies all cluster labels except system and internal labels.
// It filters out:
// - kubernetes.io/* labels (system labels)
// - cluster.x-k8s.io/* labels (CAPI internal labels)
// - capi-to-argocd/* labels (controller internal labels)
// - take-along-label.capi-to-argocd.* labels (take-along markers)
// - ignore-cluster.capi-to-argocd label (ignore marker)
func buildAutoLabelCopy(clusterLabels map[string]string) map[string]string {
	copyLabels := make(map[string]string)

	for key, value := range clusterLabels {
		// Skip system and internal labels
		if strings.HasPrefix(key, "kubernetes.io/") ||
			strings.HasPrefix(key, "cluster.x-k8s.io/") ||
			strings.HasPrefix(key, "capi-to-argocd/") ||
			strings.HasPrefix(key, clusterTakeAlongKey) ||
			key == clusterIgnoreKey {
			continue
		}

		// Copy the label as-is
		copyLabels[key] = value
	}

	return copyLabels
}

// buildTakeAlongLabels returns a list of valid take-along labels from a cluster.
// If EnableAutoLabelCopy is true, it copies all cluster labels automatically.
// Otherwise, it uses the take-along label mechanism for backward compatibility.
func buildTakeAlongLabels(cluster *clusterv1.Cluster) (map[string]string, []string) {
	name := cluster.Name
	namespace := cluster.Namespace
	clusterLabels := cluster.Labels

	// If auto label copy is enabled, copy all labels except system labels
	if EnableAutoLabelCopy {
		return buildAutoLabelCopy(clusterLabels), []string{}
	}

	// Original behavior: use take-along labels
	takeAlongLabels := []string{}
	// Check labels keys that begin with clusterTakeAlongKey and extract the value after the last '/
	for k := range clusterLabels {
		l, err := extractTakeAlongLabel(k)
		if err != nil {
			return nil, []string{err.Error()}
		}

		if l != "" {
			takeAlongLabels = append(takeAlongLabels, l)
		}
	}

	takeAlongLabelsMap := make(map[string]string)

	errors := []string{}

	if len(takeAlongLabels) > 0 {
		for _, label := range takeAlongLabels {
			if label != "" {
				if _, ok := clusterLabels[label]; !ok {
					errors = append(errors, fmt.Sprintf("take-along label '%s' not found on cluster resource: %s, namespace: %s. Ignoring", label, name, namespace))

					continue
				}

				takeAlongLabelsMap[label] = clusterLabels[label]
				takeAlongLabelsMap[fmt.Sprintf("%s%s", clusterTakenFromClusterKey, label)] = ""
			}
		}
	}

	return takeAlongLabelsMap, errors
}

// BuildNamespacedName returns k8s native object identifier.
func BuildNamespacedName(s string, namespace string) types.NamespacedName {
	return types.NamespacedName{
		Name:      "cluster-" + BuildClusterName(strings.TrimSuffix(s, "-kubeconfig"), namespace),
		Namespace: ArgoNamespace,
	}
}

// BuildClusterName returns cluster name after transformations applied (with/without namespace suffix, etc).
func BuildClusterName(s string, namespace string) string {
	prefix := ""
	if EnableNamespacedNames {
		prefix += namespace + "-"
	}

	return prefix + s
}

// ConvertToSecret converts an ArgoCluster into k8s native secret object.
func (a *ArgoCluster) ConvertToSecret() (*corev1.Secret, error) {
	// if err := ValidateClusterTLSConfig(&a.ClusterConfig.TLSClientConfig); err != nil {
	// 	return nil, err
	// }
	c, err := json.Marshal(a.ClusterConfig)
	if err != nil {
		return nil, err
	}

	mergedLabels := make(map[string]string)
	for key, value := range GetArgoCommonLabels() {
		mergedLabels[key] = value
	}

	for key, value := range a.ClusterLabels {
		mergedLabels[key] = value
	}

	for key, value := range a.TakeAlongLabels {
		mergedLabels[key] = value
	}

	argoSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      a.NamespacedName.Name,
			Namespace: a.NamespacedName.Namespace,
			Labels:    mergedLabels,
		},
		Data: map[string][]byte{
			"name":   []byte(a.ClusterName),
			"server": []byte(a.ClusterServer),
			"config": c,
		},
	}

	return argoSecret, nil
}

// ValidateClusterTLSConfig validates that we got proper based64 k/v fields.
// func ValidateClusterTLSConfig(a *ArgoTLS) error {
// 	for _, v := range []string{a.CaData, a.CertData, a.KeyData} {
// 		// Check if field.value is empty
// 		if v == "" {
// 			return errors.New("missing key on ArgoTLS config")
// 		}
// 		// Check if field.value is valid b64 encoded string
// 		if _, err := b64.StdEncoding.DecodeString(v); err != nil {
// 			return err
// 		}
// 	}
// 	return nil
// }
