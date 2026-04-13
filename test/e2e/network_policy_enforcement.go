package e2e

import (
	"context"
	"fmt"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
)

var _ = g.Describe("[sig-imageregistry] image-registry operator", func() {
	g.It("[NetworkPolicy] should enforce cross-namespace ingress traffic to image-registry", func() {
		testImageRegistryCrossNamespaceIngressEnforcement()
	})
})

func testImageRegistryCrossNamespaceIngressEnforcement() {
	ctx := context.Background()
	kubeConfig := newClientConfigForTest()
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	o.Expect(err).NotTo(o.HaveOccurred())

	namespace := imageRegistryNamespace

	g.By("Getting actual registry pod IPs")
	registryPodIPs := getRunningPodIPs(ctx, kubeClient, namespace, "docker-registry=default")
	o.Expect(registryPodIPs).NotTo(o.BeEmpty(), "no running registry pods found")

	g.By("Creating a temporary namespace for cross-namespace client pods")
	clientNS := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-xns-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", clientNS)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, clientNS, metav1.DeleteOptions{})
	})

	g.By("Testing cross-namespace ingress: test namespace -> registry:5000 (all namespaces allowed)")
	expectConnectivity(ctx, kubeClient, clientNS, map[string]string{"test": "arbitrary-client"}, registryPodIPs, 5000, true)

	g.By("Testing cross-namespace ingress: second test namespace -> registry:5000")
	clientNS2 := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-xns2-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", clientNS2)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, clientNS2, metav1.DeleteOptions{})
	})
	expectConnectivity(ctx, kubeClient, clientNS2, map[string]string{"test": "second-client"}, registryPodIPs, 5000, true)
}
