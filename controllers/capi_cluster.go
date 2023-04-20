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
	CertData string `yaml:"client-certificate-data"`
	KeyData  string `yaml:"client-key-data"`
}

// NewCapiCluster returns an empty CapiCluster type.
func NewCapiCluster(name, namespace string) *CapiCluster {
	return &CapiCluster{
		Name:       name,
		Namespace:  namespace,
		KubeConfig: KubeConfig{},
	}
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
	return strings.HasSuffix(n.Name, "-kubeconfig")
}
