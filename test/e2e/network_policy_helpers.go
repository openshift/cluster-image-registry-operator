package e2e

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
)

func newClientConfigForTest() *rest.Config {
	g.GinkgoHelper()
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{
		ClusterInfo: api.Cluster{InsecureSkipTLSVerify: true},
	})
	config, err := clientConfig.ClientConfig()
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to load kubeconfig")
	g.GinkgoWriter.Printf("using cluster at %s\n", config.Host)
	return config
}

func getNetworkPolicy(ctx context.Context, client kubernetes.Interface, namespace, name string) *networkingv1.NetworkPolicy {
	g.GinkgoHelper()
	policy, err := client.NetworkingV1().NetworkPolicies(namespace).Get(ctx, name, metav1.GetOptions{})
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to get NetworkPolicy %s/%s", namespace, name)
	return policy
}

func requireDefaultDenyAll(policy *networkingv1.NetworkPolicy) {
	g.GinkgoHelper()
	if len(policy.Spec.PodSelector.MatchLabels) != 0 || len(policy.Spec.PodSelector.MatchExpressions) != 0 {
		g.Fail(fmt.Sprintf("%s/%s: expected empty podSelector", policy.Namespace, policy.Name))
	}

	policyTypes := sets.New[string]()
	for _, policyType := range policy.Spec.PolicyTypes {
		policyTypes.Insert(string(policyType))
	}
	if !policyTypes.Has(string(networkingv1.PolicyTypeIngress)) || !policyTypes.Has(string(networkingv1.PolicyTypeEgress)) {
		g.Fail(fmt.Sprintf("%s/%s: expected both Ingress and Egress policyTypes, got %v", policy.Namespace, policy.Name, policy.Spec.PolicyTypes))
	}
	if len(policy.Spec.Ingress) != 0 || len(policy.Spec.Egress) != 0 {
		g.Fail(fmt.Sprintf("%s/%s: expected no ingress/egress rules for default-deny-all, got ingress=%d egress=%d",
			policy.Namespace, policy.Name, len(policy.Spec.Ingress), len(policy.Spec.Egress)))
	}
}

func requirePodSelectorLabel(policy *networkingv1.NetworkPolicy, key, value string) {
	g.GinkgoHelper()
	actual, ok := policy.Spec.PodSelector.MatchLabels[key]
	if !ok || actual != value {
		g.Fail(fmt.Sprintf("%s/%s: expected podSelector %s=%s, got %v", policy.Namespace, policy.Name, key, value, policy.Spec.PodSelector.MatchLabels))
	}
}

func requirePort(policy *networkingv1.NetworkPolicy, direction string, protocol corev1.Protocol, port int32) {
	g.GinkgoHelper()
	var found bool
	switch direction {
	case "ingress":
		for _, rule := range policy.Spec.Ingress {
			if hasPort(rule.Ports, protocol, port) {
				found = true
				break
			}
		}
	case "egress":
		for _, rule := range policy.Spec.Egress {
			if hasPort(rule.Ports, protocol, port) {
				found = true
				break
			}
		}
	}
	if !found {
		g.Fail(fmt.Sprintf("%s/%s: expected %s port %s/%d", policy.Namespace, policy.Name, direction, protocol, port))
	}
}

func hasPort(ports []networkingv1.NetworkPolicyPort, protocol corev1.Protocol, port int32) bool {
	for _, p := range ports {
		if p.Port == nil || p.Port.IntValue() != int(port) {
			continue
		}
		pProto := corev1.ProtocolTCP
		if p.Protocol != nil {
			pProto = *p.Protocol
		}
		if pProto == protocol {
			return true
		}
	}
	return false
}

func ingressPeersForPort(rules []networkingv1.NetworkPolicyIngressRule, port int32) (peers []networkingv1.NetworkPolicyPeer, allowAll bool) {
	for _, rule := range rules {
		if !hasPort(rule.Ports, corev1.ProtocolTCP, port) {
			continue
		}
		if len(rule.From) == 0 {
			allowAll = true
		}
		peers = append(peers, rule.From...)
	}
	return peers, allowAll
}

func hasPeerFromNamespace(peers []networkingv1.NetworkPolicyPeer, namespace string) bool {
	for _, peer := range peers {
		if namespaceSelectorMatches(peer.NamespaceSelector, namespace) {
			return true
		}
	}
	return false
}

func hasPeerFromAllNamespaces(peers []networkingv1.NetworkPolicyPeer) bool {
	for _, peer := range peers {
		if peer.NamespaceSelector != nil &&
			len(peer.NamespaceSelector.MatchLabels) == 0 &&
			len(peer.NamespaceSelector.MatchExpressions) == 0 &&
			peer.IPBlock == nil &&
			(peer.PodSelector == nil ||
				(len(peer.PodSelector.MatchLabels) == 0 &&
					len(peer.PodSelector.MatchExpressions) == 0)) {
			return true
		}
	}
	return false
}

func hasPeerFromPolicyGroup(peers []networkingv1.NetworkPolicyPeer, policyGroupLabelKey string) bool {
	for _, peer := range peers {
		if peer.NamespaceSelector == nil || peer.NamespaceSelector.MatchLabels == nil {
			continue
		}
		if _, ok := peer.NamespaceSelector.MatchLabels[policyGroupLabelKey]; ok {
			return true
		}
	}
	return false
}

func namespaceSelectorMatches(selector *metav1.LabelSelector, namespace string) bool {
	if selector == nil {
		return false
	}
	if selector.MatchLabels != nil {
		if selector.MatchLabels["kubernetes.io/metadata.name"] == namespace {
			return true
		}
	}
	for _, expr := range selector.MatchExpressions {
		if expr.Key != "kubernetes.io/metadata.name" {
			continue
		}
		if expr.Operator != metav1.LabelSelectorOpIn {
			continue
		}
		if slices.Contains(expr.Values, namespace) {
			return true
		}
	}
	return false
}

func requireIngressFromNamespaceOrPolicyGroup(policy *networkingv1.NetworkPolicy, port int32, namespace, policyGroupLabelKey string) {
	g.GinkgoHelper()
	peers, allowAll := ingressPeersForPort(policy.Spec.Ingress, port)
	if allowAll {
		return
	}
	if hasPeerFromNamespace(peers, namespace) {
		return
	}
	if hasPeerFromPolicyGroup(peers, policyGroupLabelKey) {
		return
	}
	if hasPeerFromAllNamespaces(peers) {
		return
	}
	g.Fail(fmt.Sprintf("%s/%s: expected ingress from namespace %s or policy-group %s on port %d", policy.Namespace, policy.Name, namespace, policyGroupLabelKey, port))
}

func logIngressFromNamespaceOptional(policy *networkingv1.NetworkPolicy, port int32, namespace string) {
	g.GinkgoHelper()
	peers, _ := ingressPeersForPort(policy.Spec.Ingress, port)
	if hasPeerFromNamespace(peers, namespace) {
		g.GinkgoWriter.Printf("networkpolicy %s/%s: ingress from namespace %s present on port %d\n", policy.Namespace, policy.Name, namespace, port)
		return
	}
	g.GinkgoWriter.Printf("networkpolicy %s/%s: no ingress from namespace %s on port %d\n", policy.Namespace, policy.Name, namespace, port)
}

func logIngressHostNetworkOrAllowAll(policy *networkingv1.NetworkPolicy, port int32) {
	g.GinkgoHelper()
	peers, allowAll := ingressPeersForPort(policy.Spec.Ingress, port)
	if allowAll {
		g.GinkgoWriter.Printf("networkpolicy %s/%s: ingress allow-all present on port %d\n", policy.Namespace, policy.Name, port)
		return
	}
	if hasPeerFromPolicyGroup(peers, "policy-group.network.openshift.io/host-network") {
		g.GinkgoWriter.Printf("networkpolicy %s/%s: ingress host-network policy-group present on port %d\n", policy.Namespace, policy.Name, port)
		return
	}
	g.GinkgoWriter.Printf("networkpolicy %s/%s: no ingress allow-all/host-network rule on port %d\n", policy.Namespace, policy.Name, port)
}

func requireEgressOnlyPolicyType(policy *networkingv1.NetworkPolicy) {
	g.GinkgoHelper()
	policyTypes := sets.New[string]()
	for _, policyType := range policy.Spec.PolicyTypes {
		policyTypes.Insert(string(policyType))
	}
	if !policyTypes.Has(string(networkingv1.PolicyTypeEgress)) {
		g.Fail(fmt.Sprintf("%s/%s: expected Egress policyType, got %v", policy.Namespace, policy.Name, policy.Spec.PolicyTypes))
	}
	if policyTypes.Has(string(networkingv1.PolicyTypeIngress)) {
		g.Fail(fmt.Sprintf("%s/%s: expected only Egress policyType but also has Ingress, got %v", policy.Namespace, policy.Name, policy.Spec.PolicyTypes))
	}
}

func requireEgressAllowAll(policy *networkingv1.NetworkPolicy) {
	g.GinkgoHelper()
	if !hasEgressAllowAll(policy.Spec.Egress) {
		g.Fail(fmt.Sprintf("%s/%s: expected egress allow-all rule (empty rule {})", policy.Namespace, policy.Name))
	}
}

func hasEgressAllowAll(rules []networkingv1.NetworkPolicyEgressRule) bool {
	for _, rule := range rules {
		if len(rule.To) == 0 && len(rule.Ports) == 0 {
			return true
		}
	}
	return false
}

func requireIngressFromAllNamespaces(policy *networkingv1.NetworkPolicy, port int32) {
	g.GinkgoHelper()
	peers, _ := ingressPeersForPort(policy.Spec.Ingress, port)
	if !hasPeerFromAllNamespaces(peers) {
		g.Fail(fmt.Sprintf("%s/%s: expected ingress from all namespaces on port %d", policy.Namespace, policy.Name, port))
	}
}

func requireIngressAllowAll(policy *networkingv1.NetworkPolicy, port int32) {
	g.GinkgoHelper()
	_, allowAll := ingressPeersForPort(policy.Spec.Ingress, port)
	if !allowAll {
		g.Fail(fmt.Sprintf("%s/%s: expected ingress allow-all (no from restriction) on port %d", policy.Namespace, policy.Name, port))
	}
}

func logEgressAllowAllTCP(policy *networkingv1.NetworkPolicy) {
	g.GinkgoHelper()
	if hasEgressAllowAllTCP(policy.Spec.Egress) {
		g.GinkgoWriter.Printf("networkpolicy %s/%s: egress allow-all TCP rule present\n", policy.Namespace, policy.Name)
		return
	}
	g.GinkgoWriter.Printf("networkpolicy %s/%s: no egress allow-all TCP rule\n", policy.Namespace, policy.Name)
}

func hasEgressAllowAllTCP(rules []networkingv1.NetworkPolicyEgressRule) bool {
	for _, rule := range rules {
		if len(rule.To) != 0 {
			continue
		}
		if hasAnyTCPPort(rule.Ports) {
			return true
		}
	}
	return false
}

func hasAnyTCPPort(ports []networkingv1.NetworkPolicyPort) bool {
	if len(ports) == 0 {
		return true
	}
	for _, p := range ports {
		if p.Protocol != nil && *p.Protocol != corev1.ProtocolTCP {
			continue
		}
		return true
	}
	return false
}

func deleteAndRestoreNetworkPolicy(ctx context.Context, client kubernetes.Interface, expected *networkingv1.NetworkPolicy) {
	g.GinkgoHelper()
	namespace := expected.Namespace
	name := expected.Name
	g.GinkgoWriter.Printf("deleting NetworkPolicy %s/%s\n", namespace, name)
	o.Expect(client.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, name, metav1.DeleteOptions{})).NotTo(o.HaveOccurred())
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		current, err := client.NetworkingV1().NetworkPolicies(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return equality.Semantic.DeepEqual(expected.Spec, current.Spec), nil
	})
	o.Expect(err).NotTo(o.HaveOccurred(), "timed out waiting for NetworkPolicy %s/%s spec to be restored", namespace, name)
	g.GinkgoWriter.Printf("NetworkPolicy %s/%s spec restored after delete\n", namespace, name)
}

func mutateAndRestoreNetworkPolicy(ctx context.Context, client kubernetes.Interface, namespace, name string) {
	g.GinkgoHelper()
	original := getNetworkPolicy(ctx, client, namespace, name)
	g.GinkgoWriter.Printf("mutating NetworkPolicy %s/%s (podSelector override)\n", namespace, name)
	patch := []byte(`{"spec":{"podSelector":{"matchLabels":{"np-reconcile":"mutated"}}}}`)
	_, err := client.NetworkingV1().NetworkPolicies(namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())

	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		current := getNetworkPolicy(ctx, client, namespace, name)
		return equality.Semantic.DeepEqual(original.Spec, current.Spec), nil
	})
	o.Expect(err).NotTo(o.HaveOccurred(), "timed out waiting for NetworkPolicy %s/%s spec to be restored", namespace, name)
	g.GinkgoWriter.Printf("NetworkPolicy %s/%s spec restored\n", namespace, name)
}

func waitForPodsReadyByLabel(ctx context.Context, client kubernetes.Interface, namespace, labelSelector string) {
	g.GinkgoHelper()
	g.GinkgoWriter.Printf("waiting for pods ready in %s with selector %s\n", namespace, labelSelector)
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		for _, pod := range pods.Items {
			if !isPodReady(&pod) {
				return false, nil
			}
		}
		return true, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred(), "timed out waiting for pods in %s with selector %s to be ready", namespace, labelSelector)
}

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func logNetworkPolicyEvents(ctx context.Context, client kubernetes.Interface, namespaces []string, policyName string) {
	g.GinkgoHelper()
	found := false
	for _, namespace := range namespaces {
		events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			g.GinkgoWriter.Printf("unable to list events in %s: %v\n", namespace, err)
			continue
		}
		for _, event := range events.Items {
			isNetworkPolicyEvent := false
			if event.InvolvedObject.Kind == "NetworkPolicy" && event.InvolvedObject.Name == policyName {
				isNetworkPolicyEvent = true
			}
			if strings.Contains(event.Reason, "NetworkPolicy") {
				isNetworkPolicyEvent = true
			}
			if strings.Contains(event.Message, policyName) {
				isNetworkPolicyEvent = true
			}
			if isNetworkPolicyEvent {
				g.GinkgoWriter.Printf("event in %s: %s %s %s\n", namespace, event.Type, event.Reason, event.Message)
				found = true
			}
		}
	}
	if !found {
		g.GinkgoWriter.Printf("no NetworkPolicy events observed for %s (best-effort)\n", policyName)
	}
}

func logNetworkPolicySummary(label string, policy *networkingv1.NetworkPolicy) {
	g.GinkgoWriter.Printf("networkpolicy %s namespace=%s name=%s podSelector=%v policyTypes=%v ingress=%d egress=%d\n",
		label,
		policy.Namespace,
		policy.Name,
		policy.Spec.PodSelector.MatchLabels,
		policy.Spec.PolicyTypes,
		len(policy.Spec.Ingress),
		len(policy.Spec.Egress),
	)
}

func logNetworkPolicyDetails(label string, policy *networkingv1.NetworkPolicy) {
	g.GinkgoHelper()
	g.GinkgoWriter.Printf("networkpolicy %s details:\n", label)
	g.GinkgoWriter.Printf("  podSelector=%v policyTypes=%v\n", policy.Spec.PodSelector.MatchLabels, policy.Spec.PolicyTypes)
	for i, rule := range policy.Spec.Ingress {
		g.GinkgoWriter.Printf("  ingress[%d]: ports=%s from=%s\n", i, formatPorts(rule.Ports), formatPeers(rule.From))
	}
	for i, rule := range policy.Spec.Egress {
		g.GinkgoWriter.Printf("  egress[%d]: ports=%s to=%s\n", i, formatPorts(rule.Ports), formatPeers(rule.To))
	}
}

func formatPorts(ports []networkingv1.NetworkPolicyPort) string {
	if len(ports) == 0 {
		return "[]"
	}
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		proto := "TCP"
		if p.Protocol != nil {
			proto = string(*p.Protocol)
		}
		if p.Port == nil {
			out = append(out, fmt.Sprintf("%s:any", proto))
			continue
		}
		out = append(out, fmt.Sprintf("%s:%s", proto, p.Port.String()))
	}
	return fmt.Sprintf("[%s]", strings.Join(out, ", "))
}

func formatPeers(peers []networkingv1.NetworkPolicyPeer) string {
	if len(peers) == 0 {
		return "[]"
	}
	out := make([]string, 0, len(peers))
	for _, peer := range peers {
		ns := formatSelector(peer.NamespaceSelector)
		pod := formatSelector(peer.PodSelector)
		if ns == "" && pod == "" {
			out = append(out, "{}")
			continue
		}
		out = append(out, fmt.Sprintf("ns=%s pod=%s", ns, pod))
	}
	return fmt.Sprintf("[%s]", strings.Join(out, ", "))
}

func formatSelector(sel *metav1.LabelSelector) string {
	if sel == nil {
		return ""
	}
	if len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0 {
		return "{}"
	}
	return fmt.Sprintf("labels=%v exprs=%v", sel.MatchLabels, sel.MatchExpressions)
}

const (
	agnhostImage = "registry.k8s.io/e2e-test-images/agnhost:2.45"
)

func getRunningPodIPs(ctx context.Context, kubeClient kubernetes.Interface, namespace, labelSelector string) []string {
	g.GinkgoHelper()
	pods, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to list pods in %s with selector %s", namespace, labelSelector)

	var allIPs []string
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, podIP := range pod.Status.PodIPs {
			if podIP.IP != "" {
				allIPs = append(allIPs, podIP.IP)
			}
		}
	}
	g.GinkgoWriter.Printf("found %d running pod(s) with selector %s in %s, IPs: %v\n", len(pods.Items), labelSelector, namespace, allIPs)
	return allIPs
}

func createTestNamespace(ctx context.Context, kubeClient kubernetes.Interface, name string) string {
	g.GinkgoHelper()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"pod-security.kubernetes.io/enforce":             "restricted",
				"security.openshift.io/scc.podSecurityLabelSync": "false",
			},
		},
	}
	_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	o.Expect(err).NotTo(o.HaveOccurred())
	return name
}

func isIPv6(ip string) bool {
	return net.ParseIP(ip) != nil && strings.Contains(ip, ":")
}

func formatIPPort(ip string, port int32) string {
	if isIPv6(ip) {
		return fmt.Sprintf("[%s]:%d", ip, port)
	}
	return fmt.Sprintf("%s:%d", ip, port)
}

func expectConnectivity(ctx context.Context, kubeClient kubernetes.Interface, namespace string, clientLabels map[string]string, serverIPs []string, port int32, shouldSucceed bool) {
	g.GinkgoHelper()
	for _, ip := range serverIPs {
		family := "IPv4"
		if isIPv6(ip) {
			family = "IPv6"
		}
		g.GinkgoWriter.Printf("checking %s connectivity %s -> %s expected=%t\n", family, namespace, formatIPPort(ip, port), shouldSucceed)
		expectConnectivityForIP(ctx, kubeClient, namespace, clientLabels, ip, port, shouldSucceed)
	}
}

func expectConnectivityForIP(ctx context.Context, kubeClient kubernetes.Interface, namespace string, clientLabels map[string]string, serverIP string, port int32, shouldSucceed bool) {
	g.GinkgoHelper()
	podName, cleanup, err := createConnectivityClientPod(ctx, kubeClient, namespace, clientLabels, serverIP, port)
	o.Expect(err).NotTo(o.HaveOccurred())
	defer cleanup()

	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		succeeded, err := readConnectivityResult(ctx, kubeClient, namespace, podName)
		if err != nil {
			g.GinkgoWriter.Printf("waiting for connectivity result: %v\n", err)
			return false, nil
		}
		return succeeded == shouldSucceed, nil
	})
	o.Expect(err).NotTo(o.HaveOccurred())
	g.GinkgoWriter.Printf("connectivity %s/%s expected=%t\n", namespace, formatIPPort(serverIP, port), shouldSucceed)
}

func createConnectivityClientPod(ctx context.Context, kubeClient kubernetes.Interface, namespace string, labels map[string]string, serverIP string, port int32) (string, func(), error) {
	name := fmt.Sprintf("np-client-%s", rand.String(5))
	target := formatIPPort(serverIP, port)

	g.GinkgoWriter.Printf("creating client pod %s/%s to probe %s\n", namespace, name, target)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"openshift.io/required-scc": "nonroot-v2",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   ptr.To(true),
				RunAsUser:      ptr.To(int64(1001)),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{
				{
					Name:  "connect",
					Image: agnhostImage,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						RunAsNonRoot:             ptr.To(true),
						RunAsUser:                ptr.To(int64(1001)),
					},
					Command: []string{"/bin/sh", "-c"},
					Args: []string{
						fmt.Sprintf("while true; do if /agnhost connect --protocol=tcp --timeout=5s %s 2>/dev/null; then echo CONN_OK; else echo CONN_FAIL; fi; sleep 3; done", target),
					},
				},
			},
		},
	}

	_, err := kubeClient.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", nil, err
	}

	if err := waitForPodReady(ctx, kubeClient, namespace, name); err != nil {
		logPodDebugInfo(ctx, kubeClient, namespace, name)
		_ = kubeClient.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
		return "", nil, fmt.Errorf("client pod %s/%s never became ready: %w", namespace, name, err)
	}

	cleanup := func() {
		g.GinkgoWriter.Printf("deleting client pod %s/%s\n", namespace, name)
		_ = kubeClient.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	}

	return name, cleanup, nil
}

func readConnectivityResult(ctx context.Context, kubeClient kubernetes.Interface, namespace, podName string) (bool, error) {
	tailLines := int64(1)
	raw, err := kubeClient.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		TailLines: &tailLines,
	}).DoRaw(ctx)
	if err != nil {
		return false, err
	}

	line := strings.TrimSpace(string(raw))
	if line == "" {
		return false, fmt.Errorf("no connectivity result yet from pod %s/%s", namespace, podName)
	}

	g.GinkgoWriter.Printf("client pod %s/%s result=%s\n", namespace, podName, line)
	return line == "CONN_OK", nil
}

func waitForPodReady(ctx context.Context, kubeClient kubernetes.Interface, namespace, name string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		pod, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return false, fmt.Errorf("pod %s/%s terminated with phase %s", namespace, name, pod.Status.Phase)
		}
		if pod.Status.Phase != corev1.PodRunning {
			g.GinkgoWriter.Printf("pod %s/%s phase=%s\n", namespace, name, pod.Status.Phase)
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					g.GinkgoWriter.Printf("  container %s waiting: %s - %s\n", cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
				}
			}
			return false, nil
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

func logPodDebugInfo(ctx context.Context, kubeClient kubernetes.Interface, namespace, name string) {
	pod, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		g.GinkgoWriter.Printf("failed to get pod %s/%s for debug: %v\n", namespace, name, err)
		return
	}
	g.GinkgoWriter.Printf("pod %s/%s debug: phase=%s\n", namespace, name, pod.Status.Phase)
	for _, cond := range pod.Status.Conditions {
		g.GinkgoWriter.Printf("  condition %s=%s reason=%s message=%s\n", cond.Type, cond.Status, cond.Reason, cond.Message)
	}
	for _, cs := range pod.Status.ContainerStatuses {
		g.GinkgoWriter.Printf("  container %s ready=%t restarts=%d\n", cs.Name, cs.Ready, cs.RestartCount)
		if cs.State.Waiting != nil {
			g.GinkgoWriter.Printf("    waiting: %s - %s\n", cs.State.Waiting.Reason, cs.State.Waiting.Message)
		}
		if cs.State.Terminated != nil {
			g.GinkgoWriter.Printf("    terminated: %s - %s (exit=%d)\n", cs.State.Terminated.Reason, cs.State.Terminated.Message, cs.State.Terminated.ExitCode)
		}
	}
	events, err := kubeClient.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Pod", name),
	})
	if err != nil {
		g.GinkgoWriter.Printf("failed to get events for pod %s/%s: %v\n", namespace, name, err)
		return
	}
	for _, event := range events.Items {
		g.GinkgoWriter.Printf("  event: %s %s %s\n", event.Type, event.Reason, event.Message)
	}
}
