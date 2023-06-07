package test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/helm"
	http_helper "github.com/gruntwork-io/terratest/modules/http-helper"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/random"
	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestE2ECapi2Argo(t *testing.T) {
	// Path to the helm chart we will test
	helmChartPath := "../charts/capi2argo-cluster-operator"

	// Setup the kubectl config and context. Here we choose to use the defaults, which is:
	// - HOME/.kube/config for the kubectl config file
	// - Current context of the kubectl config file
	// We also specify that we are working in the default namespace (required to get the Pod)
	// kubectlOptions := k8s.NewKubectlOptions("", "", "default")

	// We generate a unique release name so that we can refer to after deployment.
	// By doing so, we can schedule the delete call here so that at the end of the test, we run
	// `helm delete RELEASE_NAME` to clean up any resources that were created.
	releaseName := fmt.Sprintf("caco-%s", strings.ToLower(random.UniqueId()))

	// Setup the chart options. For this test, we will set the following input values:
	options := &helm.Options{
		SetValues: map[string]string{
			"fullnameOverride": releaseName,
			"image.tag":        "dev",
		},
		ExtraArgs: map[string][]string{
			"upgrade":    {"--wait"},
		},
	}

	// Clean up deployment on when finished
	defer helm.Delete(t, options, releaseName, true)

	// Deploy the chart using `helm upgrade`. Note that we use the version without `E`, since we want to assert the
	// install succeeds without any errors.
	helm.Upgrade(t, options, helmChartPath, releaseName)

	// Now that the chart is deployed, verify the deployment and get the name of working pod.
	// filter := &metav1.ListOptions{
	// 	LabelSelector: fmt.Sprintf("app.kubernetes.io/instance=%s", releaseName),
	// }
	// pod := k8s.ListPods(t, kubectlOptions, *filter)
	// podName := pod[0].Name

	// This function will open a tunnel to the Pod and hit astrolavos container endpoint.
	// VerifyExposedMetrics(t, kubectlOptions, podName)
}

// VerifyExposedMetrics will open a tunnel to the Pod and hit the endpoint to verify the metrics are exposed.
func VerifyExposedMetrics(t *testing.T, kubectlOptions *k8s.KubectlOptions, podName string) {
	// Wait for the pod to come up. It takes some time for the Pod to start, so retry a few times.
	retries := 15
	sleep := 5 * time.Second
	k8s.WaitUntilPodAvailable(t, kubectlOptions, podName, retries, sleep)

	// We will first open a tunnel to the pod, making sure to close it at the end of the test.
	tunnel := k8s.NewTunnel(kubectlOptions, k8s.ResourceTypePod, podName, 0, 3000)
	defer tunnel.Close()
	tunnel.ForwardPort(t)

	// Now that we have the tunnel, we will verify that we get back a 200 OK on /metrics endpoint plus metrics
	// for our defined target.
	// It takes some time for the Pod to start, so retry a few times.
	endpoint := fmt.Sprintf("http://%s/metrics", tunnel.Endpoint())
	http_helper.HttpGetWithRetryWithCustomValidation(
		t,
		endpoint,
		nil,
		retries,
		sleep,
		func(statusCode int, body string) bool {
			return statusCode == 200 && strings.Contains(body, "endpoint=\"https://kubernetes\"")
		},
	)
}
