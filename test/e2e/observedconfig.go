package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/test/library"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

var _ = g.Describe("[sig-imageregistry][Serial] Image registry TLS configuration", func() {
	defer g.GinkgoRecover()

	var te framework.TestEnv

	// OTE tests run against a live cluster where the operator and registry
	// are already deployed.  BeforeEach only creates a client and verifies
	// the registry is available.  Heavy stabilization waits (KAS + registry
	// pods) are done inside the individual tests that modify cluster config.
	g.BeforeEach(func() {
		fmt.Fprintf(g.GinkgoWriter, "=== BeforeEach: creating ginkgo test environment ===\n")
		gte := newGinkgoTestEnv()
		te = gte

		fmt.Fprintf(g.GinkgoWriter, "BeforeEach: verifying image registry is available\n")
		framework.WaitUntilImageRegistryIsAvailable(te)
		fmt.Fprintf(g.GinkgoWriter, "=== BeforeEach: setup complete ===\n")
	})

	g.AfterEach(func() {
		if te != nil && te.Failed() {
			fmt.Fprintf(g.GinkgoWriter, "=== AfterEach: test failed, dumping debug info ===\n")
			framework.DumpImageRegistryResource(te)
			framework.DumpOperatorLogs(context.Background(), te)
		}
	})

	g.It("should populate ObservedConfig with TLS settings from cluster APIServer [Serial]", func() {
		ctx := context.Background()

		config, err := te.Client().Configs().Get(ctx, defaults.ImageRegistryResourceName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("verifying ObservedConfig is populated")
		o.Expect(config.Spec.ObservedConfig.Raw).NotTo(o.BeEmpty(), "expected ObservedConfig to be populated")
		fmt.Fprintf(g.GinkgoWriter, "ObservedConfig raw length: %d bytes\n", len(config.Spec.ObservedConfig.Raw))

		observedConfig := map[string]any{}
		err = json.Unmarshal(config.Spec.ObservedConfig.Raw, &observedConfig)
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to unmarshal ObservedConfig")
		fmt.Fprintf(g.GinkgoWriter, "ObservedConfig contents: %s\n", string(config.Spec.ObservedConfig.Raw))

		g.By("verifying servingInfo is present in ObservedConfig")
		servingInfo, found, err := unstructured.NestedMap(observedConfig, "servingInfo")
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(found).To(o.BeTrue(), "expected servingInfo in ObservedConfig")
		fmt.Fprintf(g.GinkgoWriter, "servingInfo keys: %v\n", mapKeys(servingInfo))

		g.By("verifying minTLSVersion is set")
		minTLSVersion, found, err := unstructured.NestedString(observedConfig, "servingInfo", "minTLSVersion")
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(found).To(o.BeTrue(), "expected minTLSVersion in servingInfo")
		o.Expect(minTLSVersion).NotTo(o.BeEmpty())
		fmt.Fprintf(g.GinkgoWriter, "minTLSVersion: %s\n", minTLSVersion)

		g.By("verifying cipherSuites are set")
		cipherSuites, found, err := unstructured.NestedStringSlice(observedConfig, "servingInfo", "cipherSuites")
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(found).To(o.BeTrue(), "expected cipherSuites in servingInfo")
		o.Expect(cipherSuites).NotTo(o.BeEmpty())
		fmt.Fprintf(g.GinkgoWriter, "cipherSuites (%d): %v\n", len(cipherSuites), cipherSuites)

		g.By("verifying registry deployment has TLS environment variables")
		deployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
			ctx, defaults.ImageRegistryName, metav1.GetOptions{},
		)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(deployment.Spec.Template.Spec.Containers).NotTo(o.BeEmpty(), "deployment has no containers")

		containerEnv := deployment.Spec.Template.Spec.Containers[0].Env
		envMap := envToMap(containerEnv)
		logTLSEnvVars(envMap)

		o.Expect(envMap).To(o.HaveKey("REGISTRY_HTTP_TLS_MINVERSION"),
			"expected REGISTRY_HTTP_TLS_MINVERSION to be set in deployment")
		o.Expect(envMap["REGISTRY_HTTP_TLS_MINVERSION"]).To(o.Equal(minTLSVersion))

		o.Expect(envMap).To(o.HaveKey("OPENSHIFT_REGISTRY_HTTP_TLS_CIPHERSUITES"),
			"expected OPENSHIFT_REGISTRY_HTTP_TLS_CIPHERSUITES to be set in deployment")
		o.Expect(envMap["OPENSHIFT_REGISTRY_HTTP_TLS_CIPHERSUITES"]).NotTo(o.BeEmpty())
	})

	g.It("should update registry TLS config when cluster APIServer TLS profile changes [Serial] [apigroup:config.openshift.io]", func() {
		ctx := context.Background()

		g.By("reading current cluster APIServer TLS configuration")
		apiServer, err := te.Client().APIServers().Get(ctx, "cluster", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		// Save the original TLS profile so we can restore it later.
		originalProfile := apiServer.Spec.TLSSecurityProfile
		if originalProfile != nil {
			fmt.Fprintf(g.GinkgoWriter, "original APIServer TLS profile type: %s\n", originalProfile.Type)
		} else {
			fmt.Fprintf(g.GinkgoWriter, "original APIServer TLS profile: <nil> (cluster default)\n")
		}

		g.By("updating cluster APIServer to use Modern TLS profile (TLS 1.3 only)")
		apiServer.Spec.TLSSecurityProfile = &configv1.TLSSecurityProfile{
			Type:   configv1.TLSProfileModernType,
			Modern: &configv1.ModernTLSProfile{},
		}
		_, err = te.Client().APIServers().Update(ctx, apiServer, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		fmt.Fprintf(g.GinkgoWriter, "APIServer updated to Modern profile at %s\n", time.Now().UTC().Format(time.RFC3339))

		defer func() {
			g.By("restoring original APIServer TLS profile")
			fmt.Fprintf(g.GinkgoWriter, "cleanup: restoring original TLS profile\n")
			apiServer, err := te.Client().APIServers().Get(ctx, "cluster", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			apiServer.Spec.TLSSecurityProfile = originalProfile
			_, err = te.Client().APIServers().Update(ctx, apiServer, metav1.UpdateOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())

			g.By("waiting for cluster to stabilize after restoring TLS profile")
			waitForClusterToStabilizeAfterTLSChange(ctx, te)
			waitForTLSVersionInDeployment(ctx, te, "VersionTLS12")
			fmt.Fprintf(g.GinkgoWriter, "cleanup: original TLS profile restored successfully\n")
		}()

		g.By("waiting for cluster (KAS + image-registry) to stabilize after Modern TLS profile change")
		waitForClusterToStabilizeAfterTLSChange(ctx, te)

		g.By("waiting for registry deployment to reflect the Modern TLS profile (VersionTLS13)")
		waitForTLSVersionInDeployment(ctx, te, "VersionTLS13")

		g.By("verifying ObservedConfig was updated with Modern profile")
		config, err := te.Client().Configs().Get(ctx, defaults.ImageRegistryResourceName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(config.Spec.ObservedConfig.Raw).NotTo(o.BeEmpty())
		fmt.Fprintf(g.GinkgoWriter, "ObservedConfig after Modern profile: %s\n", string(config.Spec.ObservedConfig.Raw))

		observedConfig := map[string]any{}
		err = json.Unmarshal(config.Spec.ObservedConfig.Raw, &observedConfig)
		o.Expect(err).NotTo(o.HaveOccurred())

		minTLSVersion, found, err := unstructured.NestedString(observedConfig, "servingInfo", "minTLSVersion")
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(found).To(o.BeTrue(), "minTLSVersion not found in ObservedConfig")
		fmt.Fprintf(g.GinkgoWriter, "minTLSVersion after Modern: %s (expected VersionTLS13)\n", minTLSVersion)
		o.Expect(minTLSVersion).To(o.Equal("VersionTLS13"))

		g.By("verifying exact Modern cipher suites in ObservedConfig")
		cipherSuites, found, err := unstructured.NestedStringSlice(observedConfig, "servingInfo", "cipherSuites")
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(found).To(o.BeTrue(), "expected cipherSuites in servingInfo")
		fmt.Fprintf(g.GinkgoWriter, "cipherSuites after Modern (%d): %v\n", len(cipherSuites), cipherSuites)

		expectedModernCiphers := []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
			"TLS_CHACHA20_POLY1305_SHA256",
		}
		cipherSet := make(map[string]bool, len(cipherSuites))
		for _, c := range cipherSuites {
			cipherSet[c] = true
		}
		for _, expected := range expectedModernCiphers {
			o.Expect(cipherSet).To(o.HaveKey(expected),
				fmt.Sprintf("Modern cipher suite %q not found in ObservedConfig, got: %v", expected, cipherSuites))
		}
		fmt.Fprintf(g.GinkgoWriter, "all expected Modern cipher suites verified: %v\n", expectedModernCiphers)

		g.By("switching APIServer to Intermediate TLS profile (TLS 1.2)")
		apiServer, err = te.Client().APIServers().Get(ctx, "cluster", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		apiServer.Spec.TLSSecurityProfile = &configv1.TLSSecurityProfile{
			Type:         configv1.TLSProfileIntermediateType,
			Intermediate: &configv1.IntermediateTLSProfile{},
		}
		_, err = te.Client().APIServers().Update(ctx, apiServer, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		fmt.Fprintf(g.GinkgoWriter, "APIServer updated to Intermediate profile at %s\n", time.Now().UTC().Format(time.RFC3339))

		g.By("waiting for cluster (KAS + image-registry) to stabilize after Intermediate TLS profile change")
		waitForClusterToStabilizeAfterTLSChange(ctx, te)

		g.By("waiting for registry deployment to reflect the Intermediate TLS profile (VersionTLS12)")
		waitForTLSVersionInDeployment(ctx, te, "VersionTLS12")
		fmt.Fprintf(g.GinkgoWriter, "Intermediate profile successfully propagated\n")
	})

	g.It("should set TLS config in deployment env vars during bootstrap [Serial]", func() {
		ctx := context.Background()

		g.By("verifying the registry has the expected TLS env vars after fresh bootstrap")
		deployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
			ctx, defaults.ImageRegistryName, metav1.GetOptions{},
		)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(deployment.Spec.Template.Spec.Containers).NotTo(o.BeEmpty())
		fmt.Fprintf(g.GinkgoWriter, "deployment %s/%s generation: %d, observedGeneration: %d\n",
			deployment.Namespace, deployment.Name, deployment.Generation,
			deployment.Status.ObservedGeneration)

		containerEnv := deployment.Spec.Template.Spec.Containers[0].Env
		envMap := envToMap(containerEnv)
		logTLSEnvVars(envMap)

		o.Expect(envMap).To(o.HaveKey("REGISTRY_HTTP_TLS_MINVERSION"),
			"expected REGISTRY_HTTP_TLS_MINVERSION in deployment after bootstrap")
		o.Expect(envMap).To(o.HaveKey("REGISTRY_HTTP_TLS_CERTIFICATE"),
			"expected REGISTRY_HTTP_TLS_CERTIFICATE in deployment")
		o.Expect(envMap).To(o.HaveKey("REGISTRY_HTTP_TLS_KEY"),
			"expected REGISTRY_HTTP_TLS_KEY in deployment")

		g.By("verifying the ObservedConfig was set during bootstrap")
		config, err := te.Client().Configs().Get(ctx, defaults.ImageRegistryResourceName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(config.Spec.ObservedConfig.Raw).NotTo(o.BeEmpty(),
			"expected ObservedConfig to be populated during bootstrap")
		fmt.Fprintf(g.GinkgoWriter, "bootstrap ObservedConfig: %s\n", string(config.Spec.ObservedConfig.Raw))
	})

	g.It("should keep registry stable and not hot-loop after TLS config is set [Serial]", func() {
		g.By("verifying the operator is not hot-looping after TLS config settles")
		framework.EnsureOperatorIsNotHotLooping(te)

		g.By("verifying operator conditions remain normal")
		framework.EnsureClusterOperatorStatusIsNormal(te)
	})

	g.It("should recover TLS config after ObservedConfig is cleared [Serial]", func() {
		ctx := context.Background()

		// Log the ObservedConfig before clearing for comparison.
		configBefore, err := te.Client().Configs().Get(ctx, defaults.ImageRegistryResourceName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		fmt.Fprintf(g.GinkgoWriter, "ObservedConfig before clearing: %s\n", string(configBefore.Spec.ObservedConfig.Raw))

		g.By("clearing ObservedConfig from the registry config")
		_, err = te.Client().Configs().Patch(
			ctx,
			defaults.ImageRegistryResourceName,
			types.MergePatchType,
			[]byte(`{"spec":{"observedConfig":null}}`),
			metav1.PatchOptions{},
		)
		o.Expect(err).NotTo(o.HaveOccurred())
		fmt.Fprintf(g.GinkgoWriter, "ObservedConfig cleared at %s\n", time.Now().UTC().Format(time.RFC3339))

		g.By("waiting for the operator to repopulate ObservedConfig")
		var lastRawLen int
		err = wait.PollUntilContextTimeout(ctx, 5*time.Second, framework.AsyncOperationTimeout, false,
			func(ctx context.Context) (bool, error) {
				config, err := te.Client().Configs().Get(ctx, defaults.ImageRegistryResourceName, metav1.GetOptions{})
				if err != nil {
					fmt.Fprintf(g.GinkgoWriter, "  poll: error fetching config: %v\n", err)
					return false, err
				}
				lastRawLen = len(config.Spec.ObservedConfig.Raw)
				if lastRawLen > 0 {
					fmt.Fprintf(g.GinkgoWriter, "  poll: ObservedConfig repopulated (%d bytes)\n", lastRawLen)
					return true, nil
				}
				fmt.Fprintf(g.GinkgoWriter, "  poll: ObservedConfig still empty, waiting...\n")
				return false, nil
			},
		)
		o.Expect(err).NotTo(o.HaveOccurred(),
			fmt.Sprintf("ObservedConfig was not repopulated after being cleared (last raw len: %d)", lastRawLen))

		// Log the repopulated ObservedConfig.
		configAfter, err := te.Client().Configs().Get(ctx, defaults.ImageRegistryResourceName, metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		fmt.Fprintf(g.GinkgoWriter, "ObservedConfig after recovery: %s\n", string(configAfter.Spec.ObservedConfig.Raw))

		g.By("verifying deployment still has TLS env vars")
		deployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
			ctx, defaults.ImageRegistryName, metav1.GetOptions{},
		)
		o.Expect(err).NotTo(o.HaveOccurred())

		containerEnv := deployment.Spec.Template.Spec.Containers[0].Env
		envMap := envToMap(containerEnv)
		logTLSEnvVars(envMap)
		o.Expect(envMap).To(o.HaveKey("REGISTRY_HTTP_TLS_MINVERSION"),
			"expected REGISTRY_HTTP_TLS_MINVERSION after ObservedConfig recovery")
	})

	g.It("should enforce TLS version at the wire level when Modern profile is set [Serial] [apigroup:config.openshift.io]", func() {
		ctx := context.Background()

		g.By("reading current cluster APIServer TLS configuration")
		apiServer, err := te.Client().APIServers().Get(ctx, "cluster", metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())

		originalProfile := apiServer.Spec.TLSSecurityProfile
		if originalProfile != nil {
			fmt.Fprintf(g.GinkgoWriter, "original APIServer TLS profile type: %s\n", originalProfile.Type)
		} else {
			fmt.Fprintf(g.GinkgoWriter, "original APIServer TLS profile: <nil> (cluster default)\n")
		}

		g.By("updating cluster APIServer to use Modern TLS profile (TLS 1.3 only)")
		apiServer.Spec.TLSSecurityProfile = &configv1.TLSSecurityProfile{
			Type:   configv1.TLSProfileModernType,
			Modern: &configv1.ModernTLSProfile{},
		}
		_, err = te.Client().APIServers().Update(ctx, apiServer, metav1.UpdateOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		fmt.Fprintf(g.GinkgoWriter, "APIServer updated to Modern profile at %s\n", time.Now().UTC().Format(time.RFC3339))

		defer func() {
			g.By("restoring original APIServer TLS profile")
			fmt.Fprintf(g.GinkgoWriter, "cleanup: restoring original TLS profile\n")
			apiServer, err := te.Client().APIServers().Get(ctx, "cluster", metav1.GetOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())
			apiServer.Spec.TLSSecurityProfile = originalProfile
			_, err = te.Client().APIServers().Update(ctx, apiServer, metav1.UpdateOptions{})
			o.Expect(err).NotTo(o.HaveOccurred())

			g.By("waiting for cluster to stabilize after restoring TLS profile")
			waitForClusterToStabilizeAfterTLSChange(ctx, te)
			waitForTLSVersionInDeployment(ctx, te, "VersionTLS12")
			fmt.Fprintf(g.GinkgoWriter, "cleanup: original TLS profile restored successfully\n")
		}()

		g.By("waiting for cluster (KAS + image-registry) to stabilize after Modern TLS profile change")
		waitForClusterToStabilizeAfterTLSChange(ctx, te)
		waitForTLSVersionInDeployment(ctx, te, "VersionTLS13")
		fmt.Fprintf(g.GinkgoWriter, "cluster stable, ready for TLS connectivity tests\n")

		// The image registry service endpoint inside the cluster.
		serviceEndpoint := fmt.Sprintf("%s.%s.svc:%d",
			defaults.ServiceName, defaults.ImageRegistryOperatorNamespace, defaults.ContainerPort)
		fmt.Fprintf(g.GinkgoWriter, "testing TLS connectivity against service endpoint: %s\n", serviceEndpoint)

		g.By("verifying TLS 1.2 connection is rejected by Modern profile")
		cmdTLS12 := fmt.Sprintf("echo | openssl s_client -connect %s -tls1_2 2>&1 || true", serviceEndpoint)
		fmt.Fprintf(g.GinkgoWriter, "running TLS 1.2 test via oc run\n")
		runCmdTLS12 := exec.Command(
			"oc", "run", "tls-test-12",
			"--image=image-registry.openshift-image-registry.svc:5000/openshift/tools:latest",
			"--rm", "-i", "--restart=Never",
			"--command", "--", "bash", "-c", cmdTLS12,
		)
		outputTLS12, errTLS12 := runCmdTLS12.CombinedOutput()
		outputTLS12Str := string(outputTLS12)
		fmt.Fprintf(g.GinkgoWriter, "TLS 1.2 test exit error: %v\n", errTLS12)
		fmt.Fprintf(g.GinkgoWriter, "TLS 1.2 test (sanitized):\n%s\n", sanitizeOpenSSLOutput(outputTLS12Str))

		// TLS 1.2 should fail with Modern profile — look for handshake failure indicators.
		tls12Failed := strings.Contains(outputTLS12Str, "ssl handshake failure") ||
			strings.Contains(outputTLS12Str, "no protocols available") ||
			strings.Contains(outputTLS12Str, "wrong version number") ||
			strings.Contains(outputTLS12Str, "sslv3 alert handshake failure") ||
			strings.Contains(outputTLS12Str, "alert protocol version") ||
			strings.Contains(outputTLS12Str, "SSL alert number 70") ||
			strings.Contains(outputTLS12Str, "no peer certificate available") ||
			!strings.Contains(outputTLS12Str, "Certificate chain")
		o.Expect(tls12Failed).To(o.BeTrue(),
			fmt.Sprintf("TLS 1.2 connection should have been rejected by Modern profile, sanitized output:\n%s",
				sanitizeOpenSSLOutput(outputTLS12Str)))
		fmt.Fprintf(g.GinkgoWriter, "PASS: TLS 1.2 connection correctly rejected by Modern profile\n")

		g.By("verifying TLS 1.3 connection is accepted by Modern profile")
		cmdTLS13 := fmt.Sprintf("echo | openssl s_client -connect %s -tls1_3 2>&1 || true", serviceEndpoint)
		fmt.Fprintf(g.GinkgoWriter, "running TLS 1.3 test via oc run\n")
		runCmdTLS13 := exec.Command(
			"oc", "run", "tls-test-13",
			"--image=image-registry.openshift-image-registry.svc:5000/openshift/tools:latest",
			"--rm", "-i", "--restart=Never",
			"--command", "--", "bash", "-c", cmdTLS13,
		)
		outputTLS13, errTLS13 := runCmdTLS13.CombinedOutput()
		outputTLS13Str := string(outputTLS13)
		fmt.Fprintf(g.GinkgoWriter, "TLS 1.3 test exit error: %v\n", errTLS13)
		fmt.Fprintf(g.GinkgoWriter, "TLS 1.3 test (sanitized):\n%s\n", sanitizeOpenSSLOutput(outputTLS13Str))

		o.Expect(outputTLS13Str).To(o.ContainSubstring("Certificate chain"),
			fmt.Sprintf("TLS 1.3 connection should have succeeded with Modern profile, sanitized output:\n%s",
				sanitizeOpenSSLOutput(outputTLS13Str)))
		fmt.Fprintf(g.GinkgoWriter, "PASS: TLS 1.3 connection correctly accepted by Modern profile\n")
	})
})

// waitForTLSVersionInDeployment polls until the registry deployment's
// REGISTRY_HTTP_TLS_MINVERSION env var matches the expected value.
func waitForTLSVersionInDeployment(ctx context.Context, te framework.TestEnv, expectedVersion string) {
	fmt.Fprintf(g.GinkgoWriter, "waiting for REGISTRY_HTTP_TLS_MINVERSION=%s in deployment %s/%s\n",
		expectedVersion, defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryName)
	start := time.Now()
	var lastSeen string

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, framework.AsyncOperationTimeout, false,
		func(ctx context.Context) (bool, error) {
			deployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
				ctx, defaults.ImageRegistryName, metav1.GetOptions{},
			)
			if err != nil {
				fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: error fetching deployment: %v\n", time.Since(start).Round(time.Second), err)
				return false, nil
			}
			if len(deployment.Spec.Template.Spec.Containers) == 0 {
				fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: deployment has no containers yet\n", time.Since(start).Round(time.Second))
				return false, nil
			}

			containerEnv := deployment.Spec.Template.Spec.Containers[0].Env
			for _, env := range containerEnv {
				if env.Name == "REGISTRY_HTTP_TLS_MINVERSION" {
					lastSeen = env.Value
					if env.Value == expectedVersion {
						fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: REGISTRY_HTTP_TLS_MINVERSION=%s (matched)\n",
							time.Since(start).Round(time.Second), env.Value)
						return true, nil
					}
					fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: REGISTRY_HTTP_TLS_MINVERSION=%s (want %s)\n",
						time.Since(start).Round(time.Second), env.Value, expectedVersion)
					return false, nil
				}
			}
			fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: REGISTRY_HTTP_TLS_MINVERSION not found in env vars\n",
				time.Since(start).Round(time.Second))
			return false, nil
		},
	)
	o.Expect(err).NotTo(o.HaveOccurred(),
		fmt.Sprintf("timed out after %s waiting for REGISTRY_HTTP_TLS_MINVERSION=%s in deployment (last seen: %q)",
			time.Since(start).Round(time.Second), expectedVersion, lastSeen))
}

// waitForImageRegistryOperatorProgressing waits until the image-registry
// ClusterOperator enters the Progressing=True state, indicating it has
// detected a configuration change and started reconciling.
func waitForImageRegistryOperatorProgressing(ctx context.Context, te framework.TestEnv) {
	fmt.Fprintf(g.GinkgoWriter, "waiting for ClusterOperator %q to start progressing\n",
		defaults.ImageRegistryClusterOperatorResourceName)
	start := time.Now()

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 5*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			co, err := te.Client().ClusterOperators().Get(
				ctx, defaults.ImageRegistryClusterOperatorResourceName, metav1.GetOptions{},
			)
			if err != nil {
				fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: error fetching ClusterOperator: %v\n",
					time.Since(start).Round(time.Second), err)
				return false, nil
			}
			for _, c := range co.Status.Conditions {
				if c.Type == configv1.OperatorProgressing && c.Status == configv1.ConditionTrue {
					fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: operator is progressing (reason: %s, message: %s)\n",
						time.Since(start).Round(time.Second), c.Reason, c.Message)
					return true, nil
				}
			}
			logClusterOperatorConditions(co.Status.Conditions, start)
			return false, nil
		},
	)
	// Progressing may not always fire visibly; treat as non-fatal.
	if err != nil {
		fmt.Fprintf(g.GinkgoWriter, "WARNING: operator did not start progressing within %s, continuing: %v\n",
			time.Since(start).Round(time.Second), err)
	}
}

// waitForImageRegistryOperatorStable waits until the image-registry
// ClusterOperator reaches a stable state: Available=True, Progressing=False,
// and not Degraded.
func waitForImageRegistryOperatorStable(ctx context.Context, te framework.TestEnv) {
	fmt.Fprintf(g.GinkgoWriter, "waiting for ClusterOperator %q to become stable (Available=True, Progressing=False)\n",
		defaults.ImageRegistryClusterOperatorResourceName)
	start := time.Now()

	err := wait.PollUntilContextTimeout(ctx, 10*time.Second, 15*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			co, err := te.Client().ClusterOperators().Get(
				ctx, defaults.ImageRegistryClusterOperatorResourceName, metav1.GetOptions{},
			)
			if err != nil {
				fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: error fetching ClusterOperator: %v\n",
					time.Since(start).Round(time.Second), err)
				return false, nil
			}

			isAvailable := false
			isProgressing := true
			isDegraded := false

			for _, c := range co.Status.Conditions {
				switch c.Type {
				case configv1.OperatorAvailable:
					isAvailable = c.Status == configv1.ConditionTrue
				case configv1.OperatorProgressing:
					isProgressing = c.Status == configv1.ConditionTrue
				case configv1.OperatorDegraded:
					isDegraded = c.Status == configv1.ConditionTrue
				}
			}

			if isDegraded {
				fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: WARNING operator is degraded\n",
					time.Since(start).Round(time.Second))
				logClusterOperatorConditions(co.Status.Conditions, start)
				return false, nil
			}

			if isAvailable && !isProgressing {
				fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: operator is stable (Available=True, Progressing=False)\n",
					time.Since(start).Round(time.Second))
				return true, nil
			}

			fmt.Fprintf(g.GinkgoWriter, "  poll[%s]: operator not stable yet (Available=%v, Progressing=%v)\n",
				time.Since(start).Round(time.Second), isAvailable, isProgressing)
			return false, nil
		},
	)
	o.Expect(err).NotTo(o.HaveOccurred(),
		fmt.Sprintf("image-registry operator did not reach stable state after %s (Available=True, Progressing=False)",
			time.Since(start).Round(time.Second)))
}

// logClusterOperatorConditions prints all ClusterOperator conditions to
// GinkgoWriter for debugging.
func logClusterOperatorConditions(conditions []configv1.ClusterOperatorStatusCondition, start time.Time) {
	for _, c := range conditions {
		fmt.Fprintf(g.GinkgoWriter, "    condition[%s]: %s=%s reason=%s message=%q\n",
			time.Since(start).Round(time.Second), c.Type, c.Status, c.Reason, c.Message)
	}
}

// logTLSEnvVars prints all TLS-related environment variables from the
// deployment container to GinkgoWriter for debugging.
func logTLSEnvVars(envMap map[string]string) {
	tlsKeys := []string{
		"REGISTRY_HTTP_TLS_MINVERSION",
		"REGISTRY_HTTP_TLS_CERTIFICATE",
		"REGISTRY_HTTP_TLS_KEY",
		"OPENSHIFT_REGISTRY_HTTP_TLS_CIPHERSUITES",
	}
	fmt.Fprintf(g.GinkgoWriter, "deployment TLS environment variables:\n")
	for _, key := range tlsKeys {
		if val, ok := envMap[key]; ok {
			// Truncate long values (cipher suites) for readability.
			display := val
			if len(display) > 120 {
				display = display[:120] + "..."
			}
			fmt.Fprintf(g.GinkgoWriter, "  %s=%s\n", key, display)
		} else {
			fmt.Fprintf(g.GinkgoWriter, "  %s=<not set>\n", key)
		}
	}
}

// sanitizeOpenSSLOutput strips confidential data (certificates, session
// tickets, PSKs, session IDs, hex dumps) from openssl s_client output,
// keeping only the TLS connection summary useful for debugging.
func sanitizeOpenSSLOutput(raw string) string {
	var sanitized []string
	lines := strings.Split(raw, "\n")

	inCert := false
	inSessionTicket := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip certificate PEM blocks.
		if strings.Contains(trimmed, "-----BEGIN CERTIFICATE-----") {
			inCert = true
			sanitized = append(sanitized, "  [certificate body redacted]")
			continue
		}
		if strings.Contains(trimmed, "-----END CERTIFICATE-----") {
			inCert = false
			continue
		}
		if inCert {
			continue
		}

		// Skip TLS session ticket hex dump.
		if strings.Contains(trimmed, "TLS session ticket:") {
			inSessionTicket = true
			sanitized = append(sanitized, "  TLS session ticket: [redacted]")
			continue
		}
		// Session ticket hex lines look like "0000 - xx xx xx ..."
		if inSessionTicket {
			if len(trimmed) > 4 && trimmed[4] == ' ' && trimmed[0] >= '0' && trimmed[0] <= '9' {
				continue
			}
			inSessionTicket = false
		}

		// Redact session IDs, PSKs, and other sensitive fields.
		if strings.HasPrefix(trimmed, "Session-ID:") && !strings.HasPrefix(trimmed, "Session-ID-ctx:") {
			sanitized = append(sanitized, "    Session-ID: [redacted]")
			continue
		}
		if strings.HasPrefix(trimmed, "Resumption PSK:") {
			sanitized = append(sanitized, "    Resumption PSK: [redacted]")
			continue
		}
		if strings.HasPrefix(trimmed, "PSK identity:") {
			continue
		}
		if strings.HasPrefix(trimmed, "PSK identity hint:") {
			continue
		}

		// Skip "Server certificate" header (the raw cert follows).
		if trimmed == "Server certificate" {
			continue
		}

		sanitized = append(sanitized, line)
	}
	return strings.Join(sanitized, "\n")
}

// mapKeys returns the keys of a map for logging purposes.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// waitForClusterToStabilizeAfterTLSChange waits for kube-apiserver (KAS) and
// image-registry pods to stabilize after a TLS profile change.  Changing the
// APIServer TLS profile triggers a KAS rollout which must complete before the
// image-registry operator can observe the new config and roll out its own pods.
//
// Uses library.WaitForPodsToStabilizeOnTheSameRevision with a high success
// threshold so that pods must stay healthy for an extended period.
func waitForClusterToStabilizeAfterTLSChange(ctx context.Context, te framework.TestEnv) {
	const (
		successThreshold = 8
		successInterval  = 1 * time.Minute
		pollInterval     = 30 * time.Second
		timeout          = 22 * time.Minute
	)

	// 1. Wait for KAS pods to stabilize on the same revision.
	g.By("waiting for kube-apiserver pods to stabilize after TLS config change")
	kasNamespace := "openshift-kube-apiserver"
	kasSelector := "apiserver=true"
	fmt.Fprintf(g.GinkgoWriter, "stabilize: waiting for KAS pods (ns=%s, selector=%s) "+
		"(%d passes every %s, poll %s, timeout %s)\n",
		kasNamespace, kasSelector, successThreshold, successInterval, pollInterval, timeout)
	err := library.WaitForPodsToStabilizeOnTheSameRevision(
		te,
		te.Client().Pods(kasNamespace),
		kasSelector,
		successThreshold,
		successInterval,
		pollInterval,
		timeout,
	)
	o.Expect(err).NotTo(o.HaveOccurred(), "kube-apiserver pods did not stabilize after TLS config change")
	fmt.Fprintf(g.GinkgoWriter, "stabilize: KAS pods are stable\n")

	// 2. Wait for image-registry operator to become stable.
	g.By("waiting for image-registry operator to become stable after TLS config change")
	waitForImageRegistryOperatorStable(ctx, te)

	// 3. Wait for image-registry deployment to be fully rolled out.
	g.By("waiting for image-registry deployment to be fully rolled out")
	framework.WaitUntilDeploymentIsRolledOut(ctx, te, defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryName)

	// 4. Wait for image-registry pods to stabilize.
	g.By("waiting for image-registry pods to stabilize after TLS config change")
	registrySelector := "docker-registry=default"
	fmt.Fprintf(g.GinkgoWriter, "stabilize: waiting for registry pods (ns=%s, selector=%s) "+
		"(%d passes every %s, poll %s, timeout %s)\n",
		defaults.ImageRegistryOperatorNamespace, registrySelector,
		successThreshold, successInterval, pollInterval, timeout)
	err = library.WaitForPodsToStabilizeOnTheSameRevision(
		te,
		te.Client().Pods(defaults.ImageRegistryOperatorNamespace),
		registrySelector,
		successThreshold,
		successInterval,
		pollInterval,
		timeout,
	)
	o.Expect(err).NotTo(o.HaveOccurred(), "image-registry pods did not stabilize after TLS config change")
	fmt.Fprintf(g.GinkgoWriter, "stabilize: cluster is fully stable after TLS config change\n")
}
