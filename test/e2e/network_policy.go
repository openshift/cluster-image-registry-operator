package e2e

import (
	"context"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	imageRegistryNamespace     = "openshift-image-registry"
	npDefaultDenyAllPolicyName = "default-deny-all"
	npOperatorPolicyName       = "image-registry-operator-networkpolicy"
	npImageRegistryPolicyName  = "image-registry-networkpolicy"
	npImagePrunerPolicyName    = "image-pruner-networkpolicy"
)

var _ = g.Describe("[sig-imageregistry] image-registry operator", func() {
	g.It("[NetworkPolicy] should ensure image-registry NetworkPolicies are defined", func() {
		testImageRegistryNetworkPolicies()
	})
	g.It("[NetworkPolicy][Serial][Disruptive] should restore image-registry NetworkPolicies after delete or mutation [Timeout:30m]", func() {
		testImageRegistryNetworkPolicyReconcile()
	})
})

func testImageRegistryNetworkPolicies() {
	ctx := context.Background()
	g.By("Creating Kubernetes clients")
	kubeConfig := newClientConfigForTest()
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Validating default-deny-all NetworkPolicy")
	defaultDeny := getNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npDefaultDenyAllPolicyName)
	logNetworkPolicySummary("default-deny-all", defaultDeny)
	logNetworkPolicyDetails("default-deny-all", defaultDeny)
	requireDefaultDenyAll(defaultDeny)

	g.By("Validating image-registry-operator-networkpolicy")
	operatorPolicy := getNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npOperatorPolicyName)
	logNetworkPolicySummary(npOperatorPolicyName, operatorPolicy)
	logNetworkPolicyDetails(npOperatorPolicyName, operatorPolicy)
	requirePodSelectorLabel(operatorPolicy, "name", "cluster-image-registry-operator")
	requirePort(operatorPolicy, "ingress", corev1.ProtocolTCP, 60000)
	requireIngressAllowAll(operatorPolicy, 60000)
	logIngressFromNamespaceOptional(operatorPolicy, 60000, "openshift-monitoring")
	logIngressHostNetworkOrAllowAll(operatorPolicy, 60000)
	requireEgressAllowAll(operatorPolicy)
	requirePort(operatorPolicy, "egress", corev1.ProtocolTCP, 6443)
	requirePort(operatorPolicy, "egress", corev1.ProtocolTCP, 5353)
	requirePort(operatorPolicy, "egress", corev1.ProtocolUDP, 5353)
	requirePort(operatorPolicy, "egress", corev1.ProtocolTCP, 443)
	logEgressAllowAllTCP(operatorPolicy)

	g.By("Validating image-registry-networkpolicy")
	registryPolicy := getNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npImageRegistryPolicyName)
	logNetworkPolicySummary(npImageRegistryPolicyName, registryPolicy)
	logNetworkPolicyDetails(npImageRegistryPolicyName, registryPolicy)
	requirePodSelectorLabel(registryPolicy, "docker-registry", "default")
	requirePort(registryPolicy, "ingress", corev1.ProtocolTCP, 5000)
	requireIngressFromAllNamespaces(registryPolicy, 5000)
	logIngressFromNamespaceOptional(registryPolicy, 5000, "openshift-monitoring")
	requireIngressFromNamespaceOrPolicyGroup(registryPolicy, 5000, "openshift-ingress", "policy-group.network.openshift.io/ingress")
	logIngressHostNetworkOrAllowAll(registryPolicy, 5000)
	requireEgressAllowAll(registryPolicy)
	requirePort(registryPolicy, "egress", corev1.ProtocolTCP, 8443)
	requirePort(registryPolicy, "egress", corev1.ProtocolTCP, 5353)
	requirePort(registryPolicy, "egress", corev1.ProtocolUDP, 5353)
	requirePort(registryPolicy, "egress", corev1.ProtocolTCP, 443)
	logEgressAllowAllTCP(registryPolicy)

	g.By("Validating image-pruner-networkpolicy")
	prunerPolicy := getNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npImagePrunerPolicyName)
	logNetworkPolicySummary(npImagePrunerPolicyName, prunerPolicy)
	logNetworkPolicyDetails(npImagePrunerPolicyName, prunerPolicy)
	requirePodSelectorLabel(prunerPolicy, "app", "image-pruner")
	requireEgressOnlyPolicyType(prunerPolicy)
	requireEgressAllowAll(prunerPolicy)
	requirePort(prunerPolicy, "egress", corev1.ProtocolTCP, 6443)
	requirePort(prunerPolicy, "egress", corev1.ProtocolTCP, 5353)
	requirePort(prunerPolicy, "egress", corev1.ProtocolUDP, 5353)
	requirePort(prunerPolicy, "egress", corev1.ProtocolTCP, 5000)
	logEgressAllowAllTCP(prunerPolicy)

	g.By("Verifying pods are ready in image-registry namespace")
	waitForPodsReadyByLabel(ctx, kubeClient, imageRegistryNamespace, "docker-registry=default")
	waitForPodsReadyByLabel(ctx, kubeClient, imageRegistryNamespace, "name=cluster-image-registry-operator")
}

func testImageRegistryNetworkPolicyReconcile() {
	ctx := context.Background()
	g.By("Creating Kubernetes clients")
	kubeConfig := newClientConfigForTest()
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	o.Expect(err).NotTo(o.HaveOccurred())

	g.By("Capturing expected NetworkPolicy specs")
	expectedDefaultDeny := getNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npDefaultDenyAllPolicyName)
	expectedOperatorPolicy := getNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npOperatorPolicyName)
	expectedRegistryPolicy := getNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npImageRegistryPolicyName)
	expectedPrunerPolicy := getNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npImagePrunerPolicyName)

	g.By("Deleting main policies and waiting for restoration")
	deleteAndRestoreNetworkPolicy(ctx, kubeClient, expectedOperatorPolicy)
	deleteAndRestoreNetworkPolicy(ctx, kubeClient, expectedRegistryPolicy)
	deleteAndRestoreNetworkPolicy(ctx, kubeClient, expectedPrunerPolicy)

	g.By("Deleting default-deny-all policy and waiting for restoration")
	deleteAndRestoreNetworkPolicy(ctx, kubeClient, expectedDefaultDeny)

	g.By("Mutating main policies and waiting for reconciliation")
	mutateAndRestoreNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npOperatorPolicyName)
	mutateAndRestoreNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npImageRegistryPolicyName)
	mutateAndRestoreNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npImagePrunerPolicyName)

	g.By("Mutating default-deny-all policy and waiting for reconciliation")
	mutateAndRestoreNetworkPolicy(ctx, kubeClient, imageRegistryNamespace, npDefaultDenyAllPolicyName)

	g.By("Checking NetworkPolicy-related events (best-effort)")
	logNetworkPolicyEvents(ctx, kubeClient, []string{imageRegistryNamespace}, npImageRegistryPolicyName)
	logNetworkPolicyEvents(ctx, kubeClient, []string{imageRegistryNamespace}, npImagePrunerPolicyName)
	logNetworkPolicyEvents(ctx, kubeClient, []string{imageRegistryNamespace}, npOperatorPolicyName)
}
