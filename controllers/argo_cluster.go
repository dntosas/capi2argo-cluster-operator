// Package controllers implements functions for manipulating CAPI generated
// cluster secrets into Argo definitions.
package controllers

import (
	// b64 "encoding/base64"
	"encoding/json"
	// "errors"
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
	// TestKubeConfig represents
	TestKubeConfig *rest.Config
)

// GetArgoCommonLabels holds a map of labels that reconciled objects must have.
func GetArgoCommonLabels() map[string]string {
	return map[string]string{
		"capi-to-argocd/owned":           "true",
		"argocd.argoproj.io/secret-type": "cluster",
	}
}

// ArgoCluster holds all information needed for CAPI --> Argo Cluster conversion
type ArgoCluster struct {
	NamespacedName  types.NamespacedName
	ClusterName     string
	ClusterServer   string
	ClusterLabels   map[string]string
	ClusterConfig   ArgoConfig
}

// ArgoConfig represents Argo Cluster.JSON.config
type ArgoConfig struct {
	TLSClientConfig *ArgoTLS `json:"tlsClientConfig,omitempty"`
	BearerToken     *string  `json:"bearerToken,omitempty"`
}

// ArgoTLS represents Argo Cluster.JSON.config.tlsClientConfig
type ArgoTLS struct {
	CaData   *string `json:"caData,omitempty"`
	CertData *string `json:"certData,omitempty"`
	KeyData  *string `json:"keyData,omitempty"`
}

// NewArgoCluster return a new ArgoCluster
func NewArgoCluster(c *CapiCluster, s *corev1.Secret) (*ArgoCluster, error) {
	log := ctrl.Log.WithName("argoCluster")

	ExtraLabels := map[string]string{}
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



// buildTakeAlongLabels returns a list of valid take-along labels from a cluster
func buildTakeAlongLabels(cluster *clusterv1.Cluster) (map[string]string, []string) {

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
