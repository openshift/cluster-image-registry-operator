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
	g.It("[NetworkPolicy] should enforce NetworkPolicy allow/deny basics in a test namespace", func() {
		testGenericNetworkPolicyEnforcement()
	})
	g.It("[NetworkPolicy] should enforce image-registry NetworkPolicies", func() {
		testImageRegistryNetworkPolicyEnforcement()
	})
	g.It("[NetworkPolicy] should enforce default-deny blocks unmatched pods", func() {
		testDefaultDenyEnforcement()
	})
	g.It("[NetworkPolicy] should enforce cross-namespace ingress traffic to image-registry", func() {
		testImageRegistryCrossNamespaceIngressEnforcement()
	})
	g.It("[NetworkPolicy] should block unauthorized namespace traffic to image-registry", func() {
		testImageRegistryUnauthorizedNamespaceBlocking()
	})
})

func testGenericNetworkPolicyEnforcement() {
	ctx := context.Background()
	kubeConfig := newClientConfigForTest()
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Creating a temporary namespace for policy enforcement checks")
	ns := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-enforcement-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", ns)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
	})

	serverName := "np-server"
	clientLabels := map[string]string{"app": "np-client"}
	serverLabels := map[string]string{"app": "np-server"}

	g.GinkgoWriter.Printf("creating netexec server pod %s/%s\n", ns, serverName)
	serverPod := netexecPod(serverName, ns, serverLabels, 8080)
	_, err = kubeClient.CoreV1().Pods(ns).Create(ctx, serverPod, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(waitForPodReady(ctx, kubeClient, ns, serverName)).NotTo(o.HaveOccurred())

	server, err := kubeClient.CoreV1().Pods(ns).Get(ctx, serverName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(server.Status.PodIPs).NotTo(o.BeEmpty())
	serverIPs := podIPs(server)
	g.GinkgoWriter.Printf("server pod %s/%s ips=%v\n", ns, serverName, serverIPs)

	g.By("Verifying allow-all when no policies select the pod")
	expectConnectivity(ctx, kubeClient, ns, clientLabels, serverIPs, 8080, true)

	g.By("Applying default deny and verifying traffic is blocked")
	_, err = kubeClient.NetworkingV1().NetworkPolicies(ns).Create(ctx, defaultDenyPolicy("default-deny", ns), metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Adding ingress allow only and verifying traffic is still blocked (egress denied)")
	_, err = kubeClient.NetworkingV1().NetworkPolicies(ns).Create(ctx, allowIngressPolicy("allow-ingress", ns, serverLabels, clientLabels, 8080), metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	expectConnectivity(ctx, kubeClient, ns, clientLabels, serverIPs, 8080, false)

	g.By("Adding egress allow and verifying traffic is permitted")
	_, err = kubeClient.NetworkingV1().NetworkPolicies(ns).Create(ctx, allowEgressPolicy("allow-egress", ns, clientLabels, serverLabels, 8080), metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	expectConnectivity(ctx, kubeClient, ns, clientLabels, serverIPs, 8080, true)
}

func testImageRegistryNetworkPolicyEnforcement() {
	ctx := context.Background()
	kubeConfig := newClientConfigForTest()
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	o.Expect(err).NotTo(o.HaveOccurred())

	namespace := imageRegistryNamespace

	_, err = kubeClient.NetworkingV1().NetworkPolicies(namespace).Get(ctx, npImageRegistryPolicyName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Getting actual registry pod IPs")
	registryPodIPs := getRunningPodIPs(ctx, kubeClient, namespace, "docker-registry=default")
	o.Expect(registryPodIPs).NotTo(o.BeEmpty(), "no running registry pods found")
	g.GinkgoWriter.Printf("registry pod IPs: %v\n", registryPodIPs)

	g.By("Creating a temporary namespace for client pods")
	clientNS := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-reg-enforce-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", clientNS)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, clientNS, metav1.DeleteOptions{})
	})
	clientLabels := map[string]string{"test": "np-registry-client"}

	g.By("Verifying allowed ingress to registry:5000 from test namespace")
	expectConnectivity(ctx, kubeClient, clientNS, clientLabels, registryPodIPs, 5000, true)

	g.By("Verifying denied ports on registry pods from test namespace")
	for _, port := range []int32{80, 443, 8080, 9090} {
		expectConnectivity(ctx, kubeClient, clientNS, clientLabels, registryPodIPs, port, false)
	}
}

func testDefaultDenyEnforcement() {
	ctx := context.Background()
	kubeConfig := newClientConfigForTest()
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	o.Expect(err).NotTo(o.HaveOccurred())

	namespace := imageRegistryNamespace
	testLabels := map[string]string{"np-test": "default-deny-check"}

	g.By("Creating a test server pod with unique labels (not matched by any allow policy)")
	serverIPs, cleanupServer := createServerPod(ctx, kubeClient, namespace, fmt.Sprintf("np-deny-server-%s", rand.String(5)), testLabels, 8080)
	g.DeferCleanup(cleanupServer)

	g.By("Creating a temporary namespace for external client pods")
	clientNS := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-deny-ext-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", clientNS)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, clientNS, metav1.DeleteOptions{})
	})

	g.By("Verifying default-deny blocks ingress from external namespace to unmatched pods")
	expectConnectivity(ctx, kubeClient, clientNS, map[string]string{"test": "any-pod"}, serverIPs, 8080, false)

	g.By("Verifying default-deny blocks egress from unmatched pods")
	registryPodIPs := getRunningPodIPs(ctx, kubeClient, namespace, "docker-registry=default")
	o.Expect(registryPodIPs).NotTo(o.BeEmpty())
	expectConnectivity(ctx, kubeClient, namespace, testLabels, registryPodIPs, 5000, false)
}

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

	g.By("Testing cross-namespace ingress: monitoring -> registry:5000")
	expectConnectivity(ctx, kubeClient, "openshift-monitoring", map[string]string{"app.kubernetes.io/name": "prometheus"}, registryPodIPs, 5000, true)

	g.By("Testing cross-namespace ingress: test namespace -> registry:5000 (all namespaces allowed)")
	expectConnectivity(ctx, kubeClient, clientNS, map[string]string{"test": "arbitrary-client"}, registryPodIPs, 5000, true)

	g.By("Testing denied cross-namespace: test namespace -> registry on unauthorized port")
	expectConnectivity(ctx, kubeClient, clientNS, map[string]string{"test": "arbitrary-client"}, registryPodIPs, 8080, false)

	g.By("Testing egress blocking: wrong labels in image-registry namespace (default-deny blocks egress)")
	expectConnectivity(ctx, kubeClient, namespace, map[string]string{"app": "wrong-app"}, registryPodIPs, 5000, false)
}

func testImageRegistryUnauthorizedNamespaceBlocking() {
	ctx := context.Background()
	kubeConfig := newClientConfigForTest()
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	o.Expect(err).NotTo(o.HaveOccurred())

	namespace := imageRegistryNamespace

	g.By("Getting actual registry pod IPs")
	registryPodIPs := getRunningPodIPs(ctx, kubeClient, namespace, "docker-registry=default")
	o.Expect(registryPodIPs).NotTo(o.BeEmpty(), "no running registry pods found")

	g.By("Creating a temporary namespace for unauthorized client pods")
	clientNS := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-unauth-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", clientNS)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, clientNS, metav1.DeleteOptions{})
	})

	g.By("Testing allow-all ingress: registry:5000 from test namespace")
	expectConnectivity(ctx, kubeClient, clientNS, map[string]string{"test": "any-pod"}, registryPodIPs, 5000, true)

	g.By("Testing port-based blocking: unauthorized port on registry from test namespace")
	expectConnectivity(ctx, kubeClient, clientNS, map[string]string{"test": "any-pod"}, registryPodIPs, 9999, false)

	g.By("Testing multiple unauthorized ports on registry")
	for _, port := range []int32{80, 443, 8080, 8443, 22, 3306} {
		expectConnectivity(ctx, kubeClient, clientNS, map[string]string{"test": "any-pod"}, registryPodIPs, port, false)
	}

	g.By("Getting actual operator pod IPs")
	operatorPodIPs := getRunningPodIPs(ctx, kubeClient, namespace, "name=cluster-image-registry-operator")
	o.Expect(operatorPodIPs).NotTo(o.BeEmpty(), "no running operator pods found")

	g.By("Testing monitoring can reach operator:60000")
	expectConnectivity(ctx, kubeClient, "openshift-monitoring", map[string]string{"app.kubernetes.io/name": "prometheus"}, operatorPodIPs, 60000, true)

	g.By("Testing unauthorized port on operator")
	expectConnectivity(ctx, kubeClient, "openshift-monitoring", map[string]string{"app.kubernetes.io/name": "prometheus"}, operatorPodIPs, 12345, false)

	g.By("Testing egress blocking: wrong labels in image-registry namespace (default-deny blocks egress)")
	expectConnectivity(ctx, kubeClient, namespace, map[string]string{"app": "wrong-label"}, registryPodIPs, 5000, false)
}
