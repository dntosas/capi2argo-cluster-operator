package controllers

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Capi2ArgoReconciler", func() {
	var ctxm context.Context
	BeforeEach(func() {
		C2A = &Capi2Argo{
			Client: K8sClient,
			Log:    TestLog,
			Scheme: TestEnv.Scheme,
		}
		ctxm = context.Background()
	})
	AfterEach(func() {})

	Context("Reconcile capi secrets with argocd", func() {
		It("should process valid kubeconfig", func() {
			argoSecretLookUp := types.NamespacedName{Name: "cluster-valid", Namespace: ArgoNamespace}
			argoCluster := &corev1.Secret{}

			err := K8sClient.Get(ctxm, argoSecretLookUp, argoCluster)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("Calling Reconcile")
			_, err = C2A.Reconcile(ctxm, MockReconcileReq("valid-kubeconfig", TestNamespace))
			Expect(err).To(BeNil())

			err = K8sClient.Get(ctxm, argoSecretLookUp, argoCluster)
			Expect(err).To(BeNil())
		})

		It("should process already existing argo secret", func() {
			argoSecretLookUp := types.NamespacedName{Name: "cluster-valid", Namespace: ArgoNamespace}
			argoCluster, argoCluster2 := &corev1.Secret{}, &corev1.Secret{}

			err := K8sClient.Get(ctxm, argoSecretLookUp, argoCluster)
			Expect(err).To(BeNil())

			By("Calling Reconcile")
			_, err = C2A.Reconcile(ctxm, MockReconcileReq("valid-kubeconfig", TestNamespace))
			Expect(err).To(BeNil())

			err = K8sClient.Get(ctxm, argoSecretLookUp, argoCluster2)
			Expect(err).To(BeNil())

			Expect(argoCluster.ObjectMeta.ResourceVersion).To(Equal(argoCluster2.ObjectMeta.ResourceVersion))
		})

		It("should not process secret with wrong Data[key]", func() {
			By("Calling Reconcile")
			_, err := C2A.Reconcile(ctxm, MockReconcileReq("err-key-kubeconfig", TestNamespace))
			Expect(fmt.Sprint(err)).To(Equal("wrong secret key"))
		})

		It("should not process secret with wrong Type", func() {
			By("Calling Reconcile")
			_, err := C2A.Reconcile(ctxm, MockReconcileReq("err-type-kubeconfig", TestNamespace))
			Expect(fmt.Sprint(err)).To(Equal("wrong secret type"))
		})
	})
})

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
