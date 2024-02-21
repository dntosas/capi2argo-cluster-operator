package controllers

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
	"time"
)

var (
	Cfg           *rest.Config
	K8sClient     client.Client
	TestEnv       *envtest.Environment
	Ctx           context.Context
	Cancel        context.CancelFunc
	C2A           *Capi2Argo
	TestLog       = ctrl.Log.WithName("test")
	TestNamespace = "test"
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Capi2ArgoClusterOperator Controller Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	TestEnv = &envtest.Environment{}
	Cfg, err := TestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(Cfg).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme
	K8sManager, err := ctrl.NewManager(Cfg, ctrl.Options{
		// Host:   "0.0.0.0",
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	C2A = &Capi2Argo{
		Client: K8sManager.GetClient(),
		Log:    TestLog,
		Scheme: K8sManager.GetScheme(),
	}
	err = C2A.SetupWithManager(K8sManager)
	Expect(err).ToNot(HaveOccurred())

	Ctx, Cancel = context.WithCancel(context.TODO())
	go func() {
		defer GinkgoRecover()
		err = K8sManager.Start(Ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	K8sClient = K8sManager.GetClient()
	Expect(K8sClient).ToNot(BeNil())

}, 60)

var _ = AfterSuite(func() {})

func TestReconcile(t *testing.T) {
	t.Parallel()
	err := MockReconcileEnv()
	assert.Nil(t, err)

	tests := []struct {
		testName           string
		testMock           reconcile.Request
		testExpectedError  bool
		testExpectedValues map[string]string
	}{
		{"process valid secret", MockReconcileReq("valid-kubeconfig", TestNamespace), false,
			map[string]string{
				"ErrorMsg": "none",
			},
		},
		{"process existing valid secret", MockReconcileReq("cluster-test", ArgoNamespace), false,
			map[string]string{
				"ErrorMsg": "none",
			},
		},
		{"process secret with wrong Data[key]", MockReconcileReq("err-key-kubeconfig", TestNamespace), true,
			map[string]string{
				"ErrorMsg": "wrong secret key",
			},
		},
		{"process secret with wrong Type", MockReconcileReq("err-type-kubeconfig", TestNamespace), true,
			map[string]string{
				"ErrorMsg": "wrong secret type",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()
			ctxm := context.Background()
			r, err := C2A.Reconcile(ctxm, tt.testMock)
			if !tt.testExpectedError {
				assert.NotNil(t, r)
				assert.Nil(t, err)
				if tt.testExpectedValues != nil {
					assert.Nil(t, err)
				}
			} else {
				assert.NotNil(t, err)
				if assert.Error(t, err) {
					assert.Equal(t, tt.testExpectedValues["ErrorMsg"], err.Error())
				}
			}
		})
	}

	t.Cleanup(func() {
		time.Sleep(5 * time.Second)
		Cancel()
		By("tearing down the test environment")
		err = TestEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	})
}

func TestValidateObjectOwner(t *testing.T) {
	var o corev1.Secret

	o.ObjectMeta.Labels = map[string]string{
		"capi-to-argocd/owned": "true",
	}
	err := ValidateObjectOwner(o)
	assert.Nil(t, err)

	o.ObjectMeta.Labels = map[string]string{
		"capi-to-argocd/owned": "false",
	}
	err = ValidateObjectOwner(o)
	assert.NotNil(t, err)
}

func MockReconcileReq(name string, namespace string) reconcile.Request {
	r := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}
	return r
}

func MockReconcileEnv() error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: TestNamespace}}
	if err := K8sClient.Create(context.Background(), ns); err != nil {
		return err
	}

	ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ArgoNamespace}}
	if err := K8sClient.Create(context.Background(), ns); err != nil {
		return err
	}

	validMock := true
	validType := true
	validKey := true
	if err := K8sClient.Create(context.Background(), MockCapiSecret(validMock, validType, validKey, "valid-kubeconfig", TestNamespace)); err != nil {
		return err
	}

	if err := K8sClient.Create(context.Background(), MockCapiSecret(validMock, !validType, validKey, "err-type-kubeconfig", TestNamespace)); err != nil {
		return err
	}

	return K8sClient.Create(context.Background(), MockCapiSecret(validMock, validType, !validKey, "err-key-kubeconfig", TestNamespace))
}
