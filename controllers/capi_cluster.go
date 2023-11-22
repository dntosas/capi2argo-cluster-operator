package controllers

import (
	"errors"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"strings"
)

// CapiClusterSecretType represents the CAPI managed secret type.
const CapiClusterSecretType corev1.SecretType = "cluster.x-k8s.io/secret"

// CapiCluster is an one-on-one representation of KubeConfig fields.
type CapiCluster struct {
	Name       string     `yaml:"name"`
	Namespace  string     `yaml:"namespace"`
	Labels   map[string]string `yaml:"labels"`
	KubeConfig KubeConfig `yaml:"kubeConfig"`
}

// KubeConfig is an one-on-one representation of KubeConfig fields.
type KubeConfig struct {
	APIVersion string    `yaml:"apiVersion"`
	Kind       string    `yaml:"kind"`
	Clusters   []Cluster `yaml:"clusters"`
	Users      []User    `yaml:"users"`
}

// Cluster represents kubeconfig.[]Clusters.Cluster fields.
type Cluster struct {
	Name    string      `yaml:"name"`
	Cluster ClusterInfo `yaml:"cluster"`
}

// ClusterInfo represents kubeconfig.[]Clusters.Cluster.Clusterinfo fields.
type ClusterInfo struct {
	CaData string `yaml:"certificate-authority-data"`
	Server string `yaml:"server"`
}

// User represents kubeconfig.[]Users fields.
type User struct {
	Name string   `yaml:"name"`
	User UserInfo `yaml:"user"`
}

// UserInfo represents kubeconfig.[]Users.User fields.
type UserInfo struct {
	CertData *string `yaml:"client-certificate-data,omitempty"`
	KeyData  *string `yaml:"client-key-data,omitempty"`
	Token    *string `yaml:"token,omitempty"`
}

// NewCapiCluster returns an empty CapiCluster type.
func NewCapiCluster(c *clusterv1.Cluster) *CapiCluster {
	name := c.Name
	namespace := c.Namespace
	clusterLabels := c.Labels

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

	return &CapiCluster{
		Name:       name,
		Namespace:  namespace,
		KubeConfig: KubeConfig{},
	}
}

// InheritLabels
func (c *CapiCluster) InheritLabels() error {
}

// Unmarshal k8s secret into CapiCluster type.
func (c *CapiCluster) Unmarshal(s *corev1.Secret) error {
	if err := ValidateCapiSecret(s); err != nil {
		return err
	}
	err := yaml.Unmarshal(s.Data["value"], &c.KubeConfig)
	if err != nil || len(c.KubeConfig.Clusters) == 0 || len(c.KubeConfig.Users) == 0 || c.KubeConfig.APIVersion != "v1" || c.KubeConfig.Kind != "Config" {
		return errors.New("invalid KubeConfig")

	}
	return nil
}

// ValidateCapiSecret validates that we got proper defined types for a given secret.
func ValidateCapiSecret(s *corev1.Secret) error {
	if s.Type != CapiClusterSecretType {
		return errors.New("wrong secret type")
	}
	if _, ok := s.Data["value"]; !ok {
		return errors.New("wrong secret key")
	}
	return nil
}

// ValidateCapiNaming validates CAPI kubeconfig naming convention.
func ValidateCapiNaming(n types.NamespacedName) bool {
	return strings.HasSuffix(n.Name, "-kubeconfig") && !strings.HasSuffix(n.Name, "-user-kubeconfig")
}

// extractTakeAlongLabel returns the take-along label key from a cluster resource
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