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
	g.It("[NetworkPolicy] should enforce default-deny blocks unmatched pods", func() {
		testDefaultDenyEnforcement()
	})
	g.It("[NetworkPolicy] should enforce cross-namespace ingress traffic to image-registry", func() {
		testImageRegistryCrossNamespaceIngressEnforcement()
	})
	g.It("[NetworkPolicy] should enforce selective egress policy allows matched pods and blocks unmatched pods", func() {
		testSelectiveEgressDenyEnforcement()
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

func testDefaultDenyEnforcement() {
	ctx := context.Background()
	kubeConfig := newClientConfigForTest()
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Creating a namespace with default-deny-all policy")
	ns := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-deny-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", ns)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
	})

	_, err = kubeClient.NetworkingV1().NetworkPolicies(ns).Create(ctx, defaultDenyPolicy("default-deny-all", ns), metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	serverName := "np-deny-server"
	serverLabels := map[string]string{"app": "deny-server"}
	g.GinkgoWriter.Printf("creating server pod %s/%s\n", ns, serverName)
	serverPod := netexecPod(serverName, ns, serverLabels, 8080)
	_, err = kubeClient.CoreV1().Pods(ns).Create(ctx, serverPod, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(waitForPodReady(ctx, kubeClient, ns, serverName)).NotTo(o.HaveOccurred())

	server, err := kubeClient.CoreV1().Pods(ns).Get(ctx, serverName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(server.Status.PodIPs).NotTo(o.BeEmpty())
	serverIPs := podIPs(server)

	g.By("Creating an external namespace for client pods")
	clientNS := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-deny-ext-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", clientNS)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, clientNS, metav1.DeleteOptions{})
	})

	g.By("Verifying default-deny blocks ingress to server from external namespace")
	expectConnectivity(ctx, kubeClient, clientNS, map[string]string{"test": "any-pod"}, serverIPs, 8080, false)

	g.By("Verifying default-deny blocks egress within the namespace")
	expectConnectivity(ctx, kubeClient, ns, map[string]string{"test": "egress-check"}, serverIPs, 8080, false)
}

func testSelectiveEgressDenyEnforcement() {
	ctx := context.Background()
	kubeConfig := newClientConfigForTest()
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Creating namespace with default-deny and selective allow policies (mirrors image-registry pattern)")
	ns := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-sel-deny-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", ns)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
	})

	_, err = kubeClient.NetworkingV1().NetworkPolicies(ns).Create(ctx, defaultDenyPolicy("default-deny-all", ns), metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	serverName := "test-server"
	serverLabels := map[string]string{"app": "test-server"}
	g.GinkgoWriter.Printf("creating server pod %s/%s\n", ns, serverName)
	serverPod := netexecPod(serverName, ns, serverLabels, 8080)
	_, err = kubeClient.CoreV1().Pods(ns).Create(ctx, serverPod, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(waitForPodReady(ctx, kubeClient, ns, serverName)).NotTo(o.HaveOccurred())

	server, err := kubeClient.CoreV1().Pods(ns).Get(ctx, serverName, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	o.Expect(server.Status.PodIPs).NotTo(o.BeEmpty())
	serverIPs := podIPs(server)

	allowedClientLabels := map[string]string{"role": "allowed"}

	g.By("Adding allow-all-ingress to server and egress for allowed labels")
	_, err = kubeClient.NetworkingV1().NetworkPolicies(ns).Create(ctx, allowAllIngressPolicy("allow-all-ingress", ns, serverLabels, 8080), metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	_, err = kubeClient.NetworkingV1().NetworkPolicies(ns).Create(ctx, allowEgressPolicy("allow-egress", ns, allowedClientLabels, serverLabels, 8080), metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Verifying ingress is allowed from external namespace")
	clientNS := createTestNamespace(ctx, kubeClient, fmt.Sprintf("np-sel-ext-%s", rand.String(5)))
	g.DeferCleanup(func() {
		g.GinkgoWriter.Printf("deleting test namespace %s\n", clientNS)
		_ = kubeClient.CoreV1().Namespaces().Delete(ctx, clientNS, metav1.DeleteOptions{})
	})
	expectConnectivity(ctx, kubeClient, clientNS, map[string]string{"test": "any-pod"}, serverIPs, 8080, true)

	g.By("Verifying allowed labels can reach server within namespace")
	expectConnectivity(ctx, kubeClient, ns, allowedClientLabels, serverIPs, 8080, true)

	g.By("Verifying unmatched labels cannot reach server (egress blocked by default-deny)")
	expectConnectivity(ctx, kubeClient, ns, map[string]string{"role": "denied"}, serverIPs, 8080, false)
}
