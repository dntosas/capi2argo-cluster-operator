package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	testRestCfg   *rest.Config
	k8sClient     client.Client
	testEnv       *envtest.Environment
	testCtx       context.Context
	testCancel    context.CancelFunc
	testC2A       *Capi2Argo
	testLog       = ctrl.Log.WithName("test")
	testNamespace = "test"
	testConfig    = Config{
		ArgoNamespace:           "argocd",
		EnableGarbageCollection: false,
		EnableNamespacedNames:   false,
		EnableAutoLabelCopy:     false,
	}
)

func TestMain(m *testing.M) {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	// Register ClusterAPI types in the test scheme so the client can work with Cluster objects.
	utilruntime.Must(clusterv1.AddToScheme(scheme.Scheme))

	testEnv = &envtest.Environment{}

	var err error

	testRestCfg, err = testEnv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start test environment: %v\n", err)
		os.Exit(1)
	}

	k8sManager, err := ctrl.NewManager(testRestCfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create manager: %v\n", err)
		os.Exit(1)
	}

	testC2A = &Capi2Argo{
		Client: k8sManager.GetClient(),
		Log:    testLog,
		Scheme: k8sManager.GetScheme(),
		Config: testConfig,
	}

	if err = testC2A.SetupWithManager(k8sManager); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup controller: %v\n", err)
		os.Exit(1)
	}

	testCtx, testCancel = context.WithCancel(context.TODO())

	go func() {
		if err := k8sManager.Start(testCtx); err != nil {
			fmt.Fprintf(os.Stderr, "failed to start manager: %v\n", err)
		}
	}()

	k8sClient = k8sManager.GetClient()

	exitCode := m.Run()

	testCancel()
	time.Sleep(2 * time.Second)

	_ = testEnv.Stop()

	os.Exit(exitCode)
}

func TestReconcile(t *testing.T) {
	t.Parallel()

	err := MockReconcileEnv()
	require.NoError(t, err, "failed to set up reconcile test environment")

	tests := []struct {
		testName    string
		testMock    reconcile.Request
		expectedErr error
	}{
		{"process valid secret", MockReconcileReq("valid-kubeconfig", testNamespace), nil},
		{"process existing valid secret", MockReconcileReq("cluster-test", testConfig.ArgoNamespace), nil},
		{"process secret with wrong Data[key]", MockReconcileReq("err-key-kubeconfig", testNamespace), ErrWrongSecretKey},
		{"process secret with wrong Type", MockReconcileReq("err-type-kubeconfig", testNamespace), ErrWrongSecretType},
		{"process Rancher secret (Opaque type)", MockReconcileReq("rancher-valid-kubeconfig", testNamespace), nil},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()

			r, err := testC2A.Reconcile(context.Background(), tt.testMock)
			if tt.expectedErr != nil {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tt.expectedErr)
			} else {
				assert.NotNil(t, r)
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateObjectOwner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		labels        map[string]string
		expectedError bool
	}{
		{"owned by CACO", map[string]string{"capi-to-argocd/owned": "true"}, false},
		{"not owned by CACO", map[string]string{"capi-to-argocd/owned": "false"}, true},
		{"missing ownership label", map[string]string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var o corev1.Secret
			o.ObjectMeta.Labels = tt.labels

			err := ValidateObjectOwner(o)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseNamespaceList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty string returns nil", "", nil},
		{"single namespace", "default", []string{"default"}},
		{"multiple namespaces", "ns1,ns2,ns3", []string{"ns1", "ns2", "ns3"}},
		{"spaces are trimmed", " ns1 , ns2 , ns3 ", []string{"ns1", "ns2", "ns3"}},
		{"trailing comma ignored", "ns1,ns2,", []string{"ns1", "ns2"}},
		{"only commas returns nil", ",,,", nil},
		{"whitespace only returns nil", "  ,  ,  ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := parseNamespaceList(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsNamespaceAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		allowedNamespaces []string
		namespace         string
		expected          bool
	}{
		{"all namespaces allowed when list is empty", nil, "anything", true},
		{"all namespaces allowed when list is empty slice", []string{}, "anything", true},
		{"namespace in allowed list", []string{"ns1", "ns2"}, "ns1", true},
		{"namespace not in allowed list", []string{"ns1", "ns2"}, "ns3", false},
		{"single allowed namespace matches", []string{"prod"}, "prod", true},
		{"single allowed namespace rejects", []string{"prod"}, "staging", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Config{AllowedNamespaces: tt.allowedNamespaces}

			assert.Equal(t, tt.expected, cfg.IsNamespaceAllowed(tt.namespace))
		})
	}
}

func MockReconcileReq(name string, namespace string) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func MockReconcileEnv() error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	if err := k8sClient.Create(context.Background(), ns); err != nil {
		return err
	}

	ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testConfig.ArgoNamespace}}
	if err := k8sClient.Create(context.Background(), ns); err != nil {
		return err
	}

	validMock := true
	validType := true
	validKey := true

	if err := k8sClient.Create(context.Background(), MockCapiSecret(validMock, validType, validKey, "valid-kubeconfig", testNamespace)); err != nil {
		return err
	}

	if err := k8sClient.Create(context.Background(), MockCapiSecret(validMock, !validType, validKey, "err-type-kubeconfig", testNamespace)); err != nil {
		return err
	}

	if err := k8sClient.Create(context.Background(), MockCapiSecret(validMock, validType, !validKey, "err-key-kubeconfig", testNamespace)); err != nil {
		return err
	}

	// Add Rancher-style secret (Opaque type).
	return k8sClient.Create(context.Background(), MockRancherSecret(validMock, validKey, "rancher-valid-kubeconfig", testNamespace))
}
