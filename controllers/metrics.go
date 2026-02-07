package controllers

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	secretsCreatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "caco_argocd_secrets_created_total",
		Help: "Total number of ArgoCD cluster secrets created by the controller",
	})

	secretsUpdatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "caco_argocd_secrets_updated_total",
		Help: "Total number of ArgoCD cluster secrets updated by the controller",
	})

	secretsDeletedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "caco_argocd_secrets_deleted_total",
		Help: "Total number of ArgoCD cluster secrets deleted by the controller",
	})
)

func init() {
	metrics.Registry.MustRegister(
		secretsCreatedTotal,
		secretsUpdatedTotal,
		secretsDeletedTotal,
	)
}
