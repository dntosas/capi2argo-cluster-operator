package controllers

import (
	"bytes"
	"context"
	goErr "errors"
	"os"
	"strconv"

	"slices"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// EnableGarbageCollection enables experimental GC feature
	EnableGarbageCollection bool

	// EnableNamespacedNames represents a mode where the cluster name is always
	// prepended by the cluster namespace in all generated secrets
	EnableNamespacedNames bool
)

func init() {
	// Dummy configuration init.
	// TODO: Handle this as part of root config.
	ArgoNamespace = os.Getenv("ARGOCD_NAMESPACE")
	if ArgoNamespace == "" {
		ArgoNamespace = "argocd"
	}

	EnableGarbageCollection, _ = strconv.ParseBool(os.Getenv("ENABLE_GARBAGE_COLLECTION"))
	EnableNamespacedNames, _ = strconv.ParseBool(os.Getenv("ENABLE_NAMESPACED_NAMES"))
}

// Capi2Argo reconciles a Secret object
type Capi2Argo struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/status,verbs=get;update;patch

// Reconcile holds all the logic for syncing CAPI to Argo Clusters.
func (r *Capi2Argo) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("secret", req.NamespacedName)

	// TODO: Check if secret is on allowed Namespaces.

	// Validate Secret.Metadata.Name complies with CAPI pattern: <clusterName>-kubeconfig
	if !ValidateCapiNaming(req.NamespacedName) {
		return ctrl.Result{}, nil
	}

	// Fetch CapiSecret
	var capiSecret corev1.Secret
	err := r.Get(ctx, req.NamespacedName, &capiSecret)
	if err != nil {
		// If we get error reading the object - requeue the request.
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}

		// If secret is deleted and GC is enabled, mark ArgoSecret for deletion.
		if EnableGarbageCollection {
			labelSelector := map[string]string{
				"capi-to-argocd/cluster-secret-name": req.NamespacedName.Name,
				"capi-to-argocd/cluster-namespace":   req.NamespacedName.Namespace,
			}
			listOption := client.MatchingLabels(labelSelector)
			secretList := &corev1.SecretList{}
			err = r.List(context.Background(), secretList, listOption)
			if err != nil {
				log.Error(err, "Failed to list Cluster Secrets")
				return ctrl.Result{}, err
			}
			if err := r.Delete(ctx, &secretList.Items[0]); err != nil {
				log.Error(err, "Failed to delete ArgoSecret")
				return ctrl.Result{}, err
			}
			log.Info("Deleted successfully of ArgoSecret")
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.Info("Fetched CapiSecret")

	// Validate CapiSecret.type is matching CAPI convention.
	// if capiSecret.Type != "cluster.x-k8s.io/secret" {
	err = ValidateCapiSecret(&capiSecret)
	if err != nil {
		log.Info("Ignoring secret as it's missing proper CAPI type", "type", capiSecret.Type)
		return ctrl.Result{}, err
	}

	// Construct CapiCluster from CapiSecret.
	nn := strings.TrimSuffix(req.NamespacedName.Name, "-kubeconfig")
	ns := req.NamespacedName.Namespace
	capiCluster := NewCapiCluster(nn, ns)
	err = capiCluster.Unmarshal(&capiSecret)
	if err != nil {
		log.Error(err, "Failed to unmarshal CapiCluster")
		return ctrl.Result{}, err
	}

	clusterObject := &clusterv1.Cluster{}
	err = r.Get(ctx, types.NamespacedName{Name: capiSecret.Labels[clusterv1.ClusterNameLabel], Namespace: req.Namespace}, clusterObject)
	if err != nil {
		log.Info("Failed to get Cluster object", "error", err)
	}

	// Check if the cluster has the ignore label
	if validateClusterIgnoreLabel(clusterObject) {
		log.Info("The cluster has label to be ignored, skipping...")
		return ctrl.Result{}, nil
	}

	// Construct ArgoCluster from CapiCluster and CapiSecret.Metadata.
	argoCluster, err := NewArgoCluster(capiCluster, &capiSecret, clusterObject)
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

	// Represent a possible existing ArgoSecret.
	var existingSecret corev1.Secret
	var exists bool

	// Check if ArgoSecret exists.
	err = r.Get(ctx, argoCluster.NamespacedName, &existingSecret)
	if errors.IsNotFound(err) {
		exists = false
		log.Info("ArgoSecret does not exists, creating..")
	} else if err == nil {
		exists = true
		log.Info("ArgoSecret exists, checking state..")
	} else {
		log.Error(err, "Failed to fetch ArgoSecret to check if exists")
		return ctrl.Result{}, err
	}

	// Reconcile ArgoSecret:
	// - If does not exists:
	//     1) Create it.
	// - If exists:
	//     1) Parse labels and check if it is meant to be managed by the controller.
	//     2) If it is controller-managed, check if updates needed and apply them.
	switch exists {
	case false:
		if err := r.Create(ctx, argoSecret); err != nil {
			log.Error(err, "Failed to create ArgoSecret")
			return ctrl.Result{}, err
		}
		log.Info("Created new ArgoSecret")
		return ctrl.Result{}, nil

	case true:

		log.Info("Checking if ArgoSecret is managed by the Controller")
		err := ValidateObjectOwner(existingSecret)
		if err != nil {
			log.Info("Not managed by Controller, skipping...")
			return ctrl.Result{}, nil
		}

		log.Info("Checking if ArgoSecret is out-of-sync with")
		changed := false
		if !bytes.Equal(existingSecret.Data["name"], []byte(argoCluster.ClusterName)) {
			existingSecret.Data["name"] = []byte(argoCluster.ClusterName)
			changed = true
		}

		if !bytes.Equal(existingSecret.Data["server"], []byte(argoCluster.ClusterServer)) {
			existingSecret.Data["server"] = []byte(argoCluster.ClusterServer)
			changed = true
		}

		if !bytes.Equal(existingSecret.Data["config"], []byte(argoSecret.Data["config"])) {
			existingSecret.Data["config"] = []byte(argoSecret.Data["config"])
			changed = true
		}

		// Check if take-along labels from argoCluster.TakeAlongLabels exist existingSecret.Labels and have the same values.
		// If not set changed to true and update existingSecret.Labels.
		log.Info("Checking for take-along labels")
		log.Info("Take along labels", "labels", argoCluster.TakeAlongLabels)
		argoSecretTakenAlongLabels := []string{}
		for l := range argoCluster.TakeAlongLabels {
			if strings.HasPrefix(l, clusterTakenFromClusterKey) {
				key := strings.Split(l, clusterTakenFromClusterKey)[1]
				argoSecretTakenAlongLabels = append(argoSecretTakenAlongLabels, key)
			}
		}
		// Find difference between secrets prefixed with `taken-from-cluster-label.capi-to-argocd`
		// between existingSecret.Labels and argoSecretTakenAlongLabels
		// in order to handle removed 'take-from'-labels from the cluster resource
		for k := range existingSecret.Labels {
			if strings.HasPrefix(k, clusterTakenFromClusterKey) {
				key := strings.Split(k, clusterTakenFromClusterKey)[1]
				if !slices.Contains(argoSecretTakenAlongLabels, key) {
					delete(existingSecret.Labels, k)
					delete(existingSecret.Labels, key)
					changed = true
				}
			}
		}

		// Update secrets labels with current values
		for k, v := range argoCluster.TakeAlongLabels {
			// check if label exists in map
			if val, ok := existingSecret.Labels[k]; ok {
				// check if label value is the same
				if val != v {
					log.Info("Updating value of label in ArgoSecret", "label", k, "value", val)
					existingSecret.Labels[k] = v
					changed = true
				}
			} else {
				log.Info("Adding missing label in ArgoSecret", "label", k)
				existingSecret.Labels[k] = v
				changed = true
			}
		}

		if changed {
			log.Info("Updating out-of-sync ArgoSecret")
			if err := r.Update(ctx, &existingSecret); err != nil {
				log.Error(err, "Failed to update ArgoSecret")
				return ctrl.Result{}, err
			}
			log.Info("Updated successfully of ArgoSecret")
			return ctrl.Result{}, nil
		}

		log.Info("ArgoSecret is in-sync with CapiCluster, skipping...")
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager ..
func (r *Capi2Argo) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Complete(r)
}

// ValidateObjectOwner checks whether reconciled object is managed by CACO or not.
func ValidateObjectOwner(s corev1.Secret) error {
	if s.ObjectMeta.Labels["capi-to-argocd/owned"] != "true" {
		return goErr.New("not owned by CACO")
	}
	return nil
}
