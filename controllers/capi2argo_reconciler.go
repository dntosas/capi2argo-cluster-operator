package controllers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"slices"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Config holds the controller configuration.
type Config struct {
	// ArgoNamespace is the namespace where ArgoCD cluster secrets are created.
	ArgoNamespace string

	// AllowedNamespaces limits the controller to watch secrets only in the
	// listed namespaces. An empty slice means all namespaces are watched.
	AllowedNamespaces []string

	// EnableGarbageCollection enables deletion of ArgoCD secrets when the
	// corresponding CAPI secret is deleted.
	EnableGarbageCollection bool

	// EnableNamespacedNames prepends the cluster namespace to the generated
	// ArgoCD secret name to avoid collisions across namespaces.
	EnableNamespacedNames bool

	// EnableAutoLabelCopy enables automatic copying of all non-system labels
	// from CAPI Cluster resources to ArgoCD secrets.
	EnableAutoLabelCopy bool

	// EnableAutoAnnotationCopy enables automatic copying of all non-system
	// annotations from CAPI Cluster resources to ArgoCD secrets.
	EnableAutoAnnotationCopy bool
}

// LoadConfigFromEnv builds a Config from environment variables with sensible defaults.
func LoadConfigFromEnv() Config {
	argoNS := os.Getenv("ARGOCD_NAMESPACE")
	if argoNS == "" {
		argoNS = "argocd"
	}

	gc, _ := strconv.ParseBool(os.Getenv("ENABLE_GARBAGE_COLLECTION"))
	ns, _ := strconv.ParseBool(os.Getenv("ENABLE_NAMESPACED_NAMES"))
	al, _ := strconv.ParseBool(os.Getenv("ENABLE_AUTO_LABEL_COPY"))
	aa, _ := strconv.ParseBool(os.Getenv("ENABLE_AUTO_ANNOTATION_COPY"))

	return Config{
		ArgoNamespace:            argoNS,
		AllowedNamespaces:        parseNamespaceList(os.Getenv("ALLOWED_NAMESPACES")),
		EnableGarbageCollection:  gc,
		EnableNamespacedNames:    ns,
		EnableAutoLabelCopy:      al,
		EnableAutoAnnotationCopy: aa,
	}
}

// parseNamespaceList splits a comma-separated namespace string into a cleaned slice.
// An empty input returns nil (meaning all namespaces are allowed).
func parseNamespaceList(raw string) []string {
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))

	for _, p := range parts {
		ns := strings.TrimSpace(p)
		if ns != "" {
			result = append(result, ns)
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

// IsNamespaceAllowed returns true if the namespace is in the allowed list,
// or if no namespace filtering is configured (empty AllowedNamespaces).
func (c *Config) IsNamespaceAllowed(namespace string) bool {
	if len(c.AllowedNamespaces) == 0 {
		return true
	}

	return slices.Contains(c.AllowedNamespaces, namespace)
}

// Capi2Argo reconciles a Secret object.
type Capi2Argo struct {
	client.Client

	Log        logr.Logger
	Scheme     *runtime.Scheme
	SyncPeriod time.Duration
	Config     Config
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/status,verbs=get;update;patch

// Reconcile holds all the logic for syncing CAPI to Argo Clusters.
func (r *Capi2Argo) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("secret", req.NamespacedName)

	// Validate Secret.Metadata.Name complies with CAPI pattern: <clusterName>-kubeconfig.
	// Don't requeue; the watch predicate already filters non-matching secrets,
	// but this is a safety check.
	if !ValidateCapiNaming(req.NamespacedName) {
		return ctrl.Result{}, nil
	}

	// Fetch CapiSecret.
	var capiSecret corev1.Secret

	err := r.Get(ctx, req.NamespacedName, &capiSecret)
	if err != nil {
		// If we get an unexpected error reading the object, requeue the request.
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}

		// Secret was deleted. If GC is enabled, clean up the corresponding ArgoSecret.
		if r.Config.EnableGarbageCollection {
			if err := r.deleteArgoSecretByLabels(ctx, log, req.NamespacedName); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
		}

		return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
	}

	log.Info("Fetched CapiSecret")

	// Validate CapiSecret.type is matching CAPI convention.
	if err = ValidateCapiSecret(&capiSecret); err != nil {
		log.Info("Ignoring secret, missing proper CAPI type", "type", capiSecret.Type)

		return ctrl.Result{}, err
	}

	// Construct CapiCluster from CapiSecret.
	nn := strings.TrimSuffix(req.NamespacedName.Name, "-kubeconfig")
	ns := req.NamespacedName.Namespace
	capiCluster := NewCapiCluster(nn, ns)

	if err = capiCluster.Unmarshal(&capiSecret); err != nil {
		log.Error(err, "Failed to unmarshal CapiCluster")

		return ctrl.Result{}, err
	}

	clusterObject := &clusterv1.Cluster{}

	err = r.Get(ctx, types.NamespacedName{Name: capiSecret.Labels[clusterv1.ClusterNameLabel], Namespace: req.Namespace}, clusterObject)
	if err != nil {
		if apierrors.IsNotFound(err) || apimeta.IsNoMatchError(err) {
			log.Info("CAPI Cluster not found, cleaning up ArgoCD secret if exists")

			if delErr := r.deleteArgoSecretByLabels(ctx, log, req.NamespacedName); delErr != nil {
				return ctrl.Result{RequeueAfter: r.SyncPeriod}, delErr
			}

			return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
		}

		log.Error(err, "Failed to get CAPI Cluster object")

		return ctrl.Result{RequeueAfter: r.SyncPeriod}, err
	}

	// Check if the cluster has the ignore label.
	if validateClusterIgnoreLabel(clusterObject) {
		log.Info("Cluster has ignore label, skipping")

		return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
	}

	// Construct ArgoCluster from CapiCluster and CapiSecret.Metadata.
	argoCluster, err := NewArgoCluster(capiCluster, &capiSecret, clusterObject, &r.Config)
	if err != nil {
		log.Error(err, "Failed to construct ArgoCluster")

		return ctrl.Result{}, err
	}

	// Convert ArgoCluster into ArgoSecret to work natively on k8s objects.
	log = r.Log.WithValues("cluster", argoCluster.NamespacedName)

	argoSecret, err := argoCluster.ConvertToSecret()
	if err != nil {
		log.Error(err, "Failed to convert ArgoCluster to ArgoSecret")

		return ctrl.Result{}, err
	}

	// Check if ArgoSecret exists.
	var existingSecret corev1.Secret

	err = r.Get(ctx, argoCluster.NamespacedName, &existingSecret)
	if apierrors.IsNotFound(err) {
		// ArgoSecret does not exist, create it.
		log.Info("ArgoSecret does not exist, creating")

		if err := r.Create(ctx, argoSecret); err != nil {
			log.Error(err, "Failed to create ArgoSecret")

			return ctrl.Result{}, err
		}

		secretsCreatedTotal.Inc()
		log.Info("Created ArgoSecret")

		return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
	} else if err != nil {
		log.Error(err, "Failed to fetch ArgoSecret")

		return ctrl.Result{}, err
	}

	// ArgoSecret exists, check if it needs updating.
	log.Info("ArgoSecret exists, checking state")

	if err := ValidateObjectOwner(existingSecret); err != nil {
		log.Info("ArgoSecret not managed by controller, skipping")

		return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
	}

	log.Info("Checking if ArgoSecret is out-of-sync")

	changed := false

	if !bytes.Equal(existingSecret.Data["name"], []byte(argoCluster.ClusterName)) {
		existingSecret.Data["name"] = []byte(argoCluster.ClusterName)
		changed = true
	}

	if !bytes.Equal(existingSecret.Data["server"], []byte(argoCluster.ClusterServer)) {
		existingSecret.Data["server"] = []byte(argoCluster.ClusterServer)
		changed = true
	}

	if !bytes.Equal(existingSecret.Data["config"], argoSecret.Data["config"]) {
		existingSecret.Data["config"] = argoSecret.Data["config"]
		changed = true
	}

	// Synchronize take-along labels.
	if syncMetadataMap(existingSecret.Labels, argoCluster.TakeAlongLabels, clusterTakenFromClusterKey, log, "label") {
		changed = true
	}

	// Synchronize take-along annotations.
	if existingSecret.Annotations == nil {
		existingSecret.Annotations = make(map[string]string)
	}

	if syncMetadataMap(existingSecret.Annotations, argoCluster.TakeAlongAnnotations, annotationTakenFromClusterKey, log, "annotation") {
		changed = true
	}

	if changed {
		log.Info("Updating out-of-sync ArgoSecret")

		if err := r.Update(ctx, &existingSecret); err != nil {
			log.Error(err, "Failed to update ArgoSecret")

			return ctrl.Result{}, err
		}

		secretsUpdatedTotal.Inc()
		log.Info("Updated ArgoSecret")

		return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
	}

	log.Info("ArgoSecret is in-sync, skipping")

	return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
}

// SetupWithManager registers the controller with the manager and configures event filtering.
func (r *Capi2Argo) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			nn := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}

			return ValidateCapiNaming(nn) && r.Config.IsNamespaceAllowed(obj.GetNamespace())
		}))).
		Complete(r)
}

// deleteArgoSecretByLabels finds and deletes an ArgoCD secret matching the given CAPI source labels.
func (r *Capi2Argo) deleteArgoSecretByLabels(ctx context.Context, log logr.Logger, nn types.NamespacedName) error {
	labelSelector := map[string]string{
		"capi-to-argocd/cluster-secret-name": nn.Name,
		"capi-to-argocd/cluster-namespace":   nn.Namespace,
	}

	secretList := &corev1.SecretList{}

	err := r.List(ctx, secretList, client.MatchingLabels(labelSelector), client.InNamespace(r.Config.ArgoNamespace))
	if err != nil {
		log.Error(err, "Failed to list ArgoCD secrets")

		return err
	}

	if len(secretList.Items) == 0 {
		log.Info("No ArgoSecret found to delete")

		return nil
	}

	if err := r.Delete(ctx, &secretList.Items[0]); err != nil && !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to delete ArgoSecret")

		return err
	}

	secretsDeletedTotal.Inc()
	log.Info("Deleted ArgoSecret", "name", secretList.Items[0].Name)

	return nil
}

// syncMetadataMap synchronizes a desired map of key-value pairs into an existing
// metadata map (labels or annotations). It removes stale entries tracked by the
// takenFromPrefix marker and upserts current values. Returns true if any change was made.
func syncMetadataMap(existing, desired map[string]string, takenFromPrefix string, log logr.Logger, kind string) bool {
	changed := false

	// Collect keys that are currently desired via the taken-from marker.
	desiredTracked := []string{}

	for k := range desired {
		if key, ok := strings.CutPrefix(k, takenFromPrefix); ok {
			desiredTracked = append(desiredTracked, key)
		}
	}

	// Remove stale entries whose taken-from marker is no longer desired.
	for k := range existing {
		if key, ok := strings.CutPrefix(k, takenFromPrefix); ok {
			if !slices.Contains(desiredTracked, key) {
				delete(existing, k)
				delete(existing, key)

				changed = true
			}
		}
	}

	// Upsert desired entries.
	for k, v := range desired {
		if val, ok := existing[k]; ok {
			if val != v {
				log.V(1).Info("Updating "+kind+" in ArgoSecret", kind, k, "oldValue", val, "newValue", v)

				existing[k] = v
				changed = true
			}
		} else {
			log.V(1).Info("Adding "+kind+" to ArgoSecret", kind, k)

			existing[k] = v
			changed = true
		}
	}

	return changed
}

// ValidateObjectOwner checks whether reconciled object is managed by CACO or not.
func ValidateObjectOwner(s corev1.Secret) error {
	if s.ObjectMeta.Labels["capi-to-argocd/owned"] != "true" {
		return errors.New("not owned by CACO")
	}

	return nil
}
