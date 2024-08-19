package controllers

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/packages"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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

	scheme := runtime.NewScheme()
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	config := &packages.Config{
		Mode: packages.NeedModule,
	}

	clusterAPIPkgs, err := packages.Load(config, "sigs.k8s.io/cluster-api")
	Expect(err).NotTo(HaveOccurred())
	Expect(clusterAPIPkgs[0].Errors).To(BeNil())
	clusterAPIDir := clusterAPIPkgs[0].Module.Dir

	crdsPaths := []string{
		filepath.Join(clusterAPIDir, "config", "crd", "bases"),
	}
	TestEnv = &envtest.Environment{
		Scheme:            scheme,
		CRDDirectoryPaths: crdsPaths,
	}
	Cfg, err := TestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(Cfg).NotTo(BeNil())

	//+kubebuilder:scaffold:scheme
	K8sManager, err := ctrl.NewManager(Cfg, ctrl.Options{
		// Host:   "0.0.0.0",
		Scheme: scheme,
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

	err = MockReconcileEnv()
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	// Stop the controller context
	Cancel()
	err := TestEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

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
