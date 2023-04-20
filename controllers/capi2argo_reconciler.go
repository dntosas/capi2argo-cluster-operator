package controllers

import (
	"bytes"
	"context"
	goErr "errors"
	"os"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// EnableGarbageCollection enables experimental GC feature
	EnableGarbageCollection bool
)

func init() {
	// Dummy configuration init.
	// TODO: Handle this as part of root config.
	ArgoNamespace = os.Getenv("ARGOCD_NAMESPACE")
	if ArgoNamespace == "" {
		ArgoNamespace = "argocd"
	}

	EnableNamespacedNames, _ = strconv.ParseBool(os.Getenv("ENABLE_NAMESPACED_NAMES"))
	EnableGarbageCollection, _ = strconv.ParseBool(os.Getenv("ENABLE_GARBAGE_COLLECTION"))
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
		// Error reading the object - requeue the request.
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// If secret is deleted and GC is enabled, mark ArgoSecret for deletion.
		if EnableGarbageCollection {
			log.Info("Source Cluster Secret does not exist anymore, deleting..")
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
	} else {
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

	// Construct ArgoCluster from CapiCluster and CapiSecret.Metadata.
	argoCluster := NewArgoCluster(capiCluster, &capiSecret)

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
	// 		 2) If it is controller-managed, check if updates needed and apply them.
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
			log.Info("Not managed by Controller, skipping..")
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

		if changed {
			log.Info("Updating out-of-sync ArgoSecret")
			if err := r.Update(ctx, &existingSecret); err != nil {
				log.Error(err, "Failed to update ArgoSecret")
				return ctrl.Result{}, err
			}
			log.Info("Updated successfully of ArgoSecret")
			return ctrl.Result{}, nil
		}

		log.Info("ArgoSecret is in-sync with CapiCluster, skipping..")
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
