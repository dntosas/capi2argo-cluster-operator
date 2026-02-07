package controllers

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Sentinel errors for CAPI secret validation.
var (
	ErrWrongSecretType   = errors.New("wrong secret type")
	ErrWrongSecretKey    = errors.New("wrong secret key")
	ErrInvalidKubeConfig = errors.New("invalid KubeConfig")
)

// CapiClusterSecretType represents the CAPI managed secret type.
//
//nolint:gosec
const CapiClusterSecretType corev1.SecretType = "cluster.x-k8s.io/secret"

// CapiCluster is a representation of KubeConfig fields.
type CapiCluster struct {
	Name       string     `yaml:"name"`
	Namespace  string     `yaml:"namespace"`
	KubeConfig KubeConfig `yaml:"kubeConfig"`
}

// KubeConfig is a representation of KubeConfig fields.
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

// ClusterInfo represents kubeconfig.[]Clusters.Cluster.ClusterInfo fields.
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

// NewCapiCluster returns an empty CapiCluster.
func NewCapiCluster(name, namespace string) *CapiCluster {
	return &CapiCluster{
		Name:       name,
		Namespace:  namespace,
		KubeConfig: KubeConfig{},
	}
}

// Unmarshal parses a Kubernetes secret into a CapiCluster.
func (c *CapiCluster) Unmarshal(s *corev1.Secret) error {
	if err := ValidateCapiSecret(s); err != nil {
		return err
	}

	err := yaml.Unmarshal(s.Data["value"], &c.KubeConfig)
	if err != nil || len(c.KubeConfig.Clusters) == 0 || len(c.KubeConfig.Users) == 0 || c.KubeConfig.APIVersion != "v1" || c.KubeConfig.Kind != "Config" {
		return fmt.Errorf("%w: failed to parse kubeconfig for %s/%s", ErrInvalidKubeConfig, c.Namespace, c.Name)
	}

	return nil
}

// ValidateCapiSecret validates that a secret has the correct type and data keys.
// It accepts both cluster.x-k8s.io/secret (standard CAPI) and Opaque (Rancher) types.
func ValidateCapiSecret(s *corev1.Secret) error {
	switch s.Type {
	case CapiClusterSecretType:
		// Standard CAPI secret type; accepted as-is.
	case corev1.SecretTypeOpaque:
		if s.Labels == nil {
			return ErrWrongSecretType
		}
		if _, ok := s.Labels["cluster.x-k8s.io/cluster-name"]; !ok {
			return ErrWrongSecretType
		}
	default:
		return ErrWrongSecretType
	}

	if _, ok := s.Data["value"]; !ok {
		return ErrWrongSecretKey
	}

	return nil
}

// ValidateCapiNaming validates CAPI kubeconfig naming convention.
func ValidateCapiNaming(n types.NamespacedName) bool {
	return strings.HasSuffix(n.Name, "-kubeconfig") && !strings.HasSuffix(n.Name, "-user-kubeconfig")
}
