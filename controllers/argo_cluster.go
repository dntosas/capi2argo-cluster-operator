// Package controllers implements functions for manipulating CAPI generated
// cluster secrets into Argo definitions.
package controllers

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	clusterTakeAlongKey        = "take-along-label.capi-to-argocd."
	clusterTakenFromClusterKey = "taken-from-cluster-label.capi-to-argocd."
	clusterIgnoreKey           = "ignore-cluster.capi-to-argocd"

	// Annotation sync constants.
	annotationTakeAlongKey        = "take-along-annotation.capi-to-argocd."
	annotationTakenFromClusterKey = "taken-from-cluster-annotation.capi-to-argocd."
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
	NamespacedName       types.NamespacedName
	ClusterName          string
	ClusterServer        string
	ClusterLabels        map[string]string
	TakeAlongLabels      map[string]string
	TakeAlongAnnotations map[string]string
	ClusterConfig        ArgoConfig
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

// NewArgoCluster returns a new ArgoCluster constructed from CAPI resources.
func NewArgoCluster(c *CapiCluster, s *corev1.Secret, cluster *clusterv1.Cluster, cfg *Config) (*ArgoCluster, error) {
	log := ctrl.Log.WithName("argoCluster")

	takeAlongLabels := map[string]string{}
	takeAlongAnnotations := map[string]string{}

	var errList []string

	if cluster != nil {
		takeAlongLabels, errList = buildTakeAlongLabels(cluster, cfg)
		for _, e := range errList {
			log.Info(e)
		}

		var annotationErrs []string

		takeAlongAnnotations, annotationErrs = buildTakeAlongAnnotations(cluster, cfg)
		for _, e := range annotationErrs {
			log.Info(e)
		}
	}

	return &ArgoCluster{
		NamespacedName: BuildNamespacedName(s.ObjectMeta.Name, s.ObjectMeta.Namespace, cfg),
		ClusterName:    BuildClusterName(c.KubeConfig.Clusters[0].Name, s.ObjectMeta.Namespace, cfg),
		ClusterServer:  c.KubeConfig.Clusters[0].Cluster.Server,
		ClusterLabels: map[string]string{
			"capi-to-argocd/cluster-secret-name": c.Name + "-kubeconfig",
			"capi-to-argocd/cluster-namespace":   c.Namespace,
		},
		TakeAlongLabels:      takeAlongLabels,
		TakeAlongAnnotations: takeAlongAnnotations,
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

		return "", fmt.Errorf("invalid take-along label, missing key after '/': %s", key)
	}

	// Not a take-along label.
	return "", nil
}

// validateClusterIgnoreLabel returns true when the cluster has the clusterIgnoreKey label.
func validateClusterIgnoreLabel(cluster *clusterv1.Cluster) bool {
	if cluster == nil {
		return false
	}

	_, exists := cluster.Labels[clusterIgnoreKey]

	return exists
}

// buildAutoLabelCopy copies all cluster labels except system and internal labels.
// It filters out:
// - kubernetes.io/* labels (system labels)
// - cluster.x-k8s.io/* labels (CAPI internal labels)
// - capi-to-argocd/* labels (controller internal labels)
// - take-along-label.capi-to-argocd.* labels (take-along markers)
// - ignore-cluster.capi-to-argocd label (ignore marker).
func buildAutoLabelCopy(clusterLabels map[string]string) map[string]string {
	copyLabels := make(map[string]string)

	for key, value := range clusterLabels {
		if strings.HasPrefix(key, "kubernetes.io/") ||
			strings.HasPrefix(key, "cluster.x-k8s.io/") ||
			strings.HasPrefix(key, "capi-to-argocd/") ||
			strings.HasPrefix(key, clusterTakeAlongKey) ||
			key == clusterIgnoreKey {
			continue
		}

		copyLabels[key] = value
	}

	return copyLabels
}

// buildTakeAlongLabels returns valid take-along labels from a cluster.
// If Config.EnableAutoLabelCopy is true, it copies all non-system labels automatically.
// Otherwise, it uses the take-along label mechanism via buildTakeAlongMap.
func buildTakeAlongLabels(cluster *clusterv1.Cluster, cfg *Config) (map[string]string, []string) {
	if cfg.EnableAutoLabelCopy {
		return buildAutoLabelCopy(cluster.Labels), []string{}
	}

	return buildTakeAlongMap(
		cluster.Name, cluster.Namespace,
		cluster.Labels,
		clusterTakeAlongKey, clusterTakenFromClusterKey,
		"label",
	)
}

// buildTakeAlongMap is the shared implementation for take-along labels and annotations.
// It extracts keys marked with takeAlongPrefix, looks up their values in source,
// and tracks them with takenFromPrefix markers.
func buildTakeAlongMap(
	clusterName, clusterNamespace string,
	source map[string]string,
	takeAlongPrefix, takenFromPrefix string,
	kind string,
) (map[string]string, []string) {
	takeAlongKeys := []string{}

	for k := range source {
		if key, ok := strings.CutPrefix(k, takeAlongPrefix); ok {
			if key == "" {
				return nil, []string{fmt.Sprintf("invalid take-along %s, missing key after '/': %s", kind, k)}
			}

			takeAlongKeys = append(takeAlongKeys, key)
		}
	}

	result := make(map[string]string)

	var errMsgs []string

	for _, key := range takeAlongKeys {
		if _, ok := source[key]; !ok {
			errMsgs = append(errMsgs, fmt.Sprintf("take-along %s '%s' not found on cluster resource: %s, namespace: %s. Ignoring", kind, key, clusterName, clusterNamespace))

			continue
		}

		result[key] = source[key]
		result[takenFromPrefix+key] = ""
	}

	return result, errMsgs
}

// buildAutoAnnotationCopy copies all cluster annotations except system and internal annotations.
// It filters out:
// - kubernetes.io/* annotations (system)
// - cluster.x-k8s.io/* annotations (CAPI internal)
// - capi-to-argocd/* annotations (controller internal)
// - take-along-annotation.capi-to-argocd.* annotations (take-along markers)
// - kubectl.kubernetes.io/* annotations (kubectl bookkeeping).
func buildAutoAnnotationCopy(clusterAnnotations map[string]string) map[string]string {
	copyAnnotations := make(map[string]string)

	for key, value := range clusterAnnotations {
		if strings.HasPrefix(key, "kubernetes.io/") ||
			strings.HasPrefix(key, "cluster.x-k8s.io/") ||
			strings.HasPrefix(key, "capi-to-argocd/") ||
			strings.HasPrefix(key, annotationTakeAlongKey) ||
			strings.HasPrefix(key, "kubectl.kubernetes.io/") {
			continue
		}

		copyAnnotations[key] = value
	}

	return copyAnnotations
}

// buildTakeAlongAnnotations returns valid take-along annotations from a cluster.
// If Config.EnableAutoAnnotationCopy is true, it copies all non-system annotations automatically.
// Otherwise, it uses the take-along annotation mechanism via buildTakeAlongMap.
func buildTakeAlongAnnotations(cluster *clusterv1.Cluster, cfg *Config) (map[string]string, []string) {
	if cfg.EnableAutoAnnotationCopy {
		return buildAutoAnnotationCopy(cluster.Annotations), []string{}
	}

	return buildTakeAlongMap(
		cluster.Name, cluster.Namespace,
		cluster.Annotations,
		annotationTakeAlongKey, annotationTakenFromClusterKey,
		"annotation",
	)
}

// BuildNamespacedName returns the Kubernetes NamespacedName for the ArgoCD secret.
func BuildNamespacedName(s string, namespace string, cfg *Config) types.NamespacedName {
	return types.NamespacedName{
		Name:      "cluster-" + BuildClusterName(strings.TrimSuffix(s, "-kubeconfig"), namespace, cfg),
		Namespace: cfg.ArgoNamespace,
	}
}

// BuildClusterName returns the cluster name with optional namespace prefix.
func BuildClusterName(s string, namespace string, cfg *Config) string {
	prefix := ""
	if cfg.EnableNamespacedNames {
		prefix += namespace + "-"
	}

	return prefix + s
}

// ConvertToSecret converts an ArgoCluster into a Kubernetes Secret.
func (a *ArgoCluster) ConvertToSecret() (*corev1.Secret, error) {
	c, err := json.Marshal(a.ClusterConfig)
	if err != nil {
		return nil, fmt.Errorf("marshalling ArgoCluster config: %w", err)
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

	var mergedAnnotations map[string]string
	if len(a.TakeAlongAnnotations) > 0 {
		mergedAnnotations = make(map[string]string, len(a.TakeAlongAnnotations))
		for key, value := range a.TakeAlongAnnotations {
			mergedAnnotations[key] = value
		}
	}

	argoSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        a.NamespacedName.Name,
			Namespace:   a.NamespacedName.Namespace,
			Labels:      mergedLabels,
			Annotations: mergedAnnotations,
		},
		Data: map[string][]byte{
			"name":   []byte(a.ClusterName),
			"server": []byte(a.ClusterServer),
			"config": c,
		},
	}

	return argoSecret, nil
}
