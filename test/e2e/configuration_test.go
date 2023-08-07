package e2e

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	appsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryapiv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestHTTPSecretDefaulter(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
	})
	defer framework.TeardownImageRegistry(te)

	cr, err := te.Client().Configs().Get(context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if cr.Spec.HTTPSecret == "" {
		t.Errorf("got empty spec.httpSecrets, want random string")
	}

	firstSecret := cr.Spec.HTTPSecret

	if _, err := te.Client().Configs().Patch(
		context.Background(),
		defaults.ImageRegistryResourceName,
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:    "replace",
				Path:  "/spec/httpSecret",
				Value: "",
			},
		}),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("unable to reset httpSecret: %s", err)
	}

	cr = framework.WaitUntilImageRegistryConfigIsProcessed(te)
	if cr.Spec.HTTPSecret == "" {
		t.Errorf("got empty spec.httpSecrets, want it to be regenerated")
	}
	if cr.Spec.HTTPSecret == firstSecret {
		t.Errorf("regenerated spec.httpSecrets %q is the same, want it to be randomized", cr.Spec.HTTPSecret)
	}
}

func TestPodResourceConfiguration(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
		Resources: &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		NodeSelector: map[string]string{
			"node-role.kubernetes.io/master": "",
		},
		Tolerations: []corev1.Toleration{
			{
				Key:      "node-role.kubernetes.io/master",
				Operator: "Exists",
				Effect:   "NoSchedule",
			},
		},
	})
	defer framework.TeardownImageRegistry(te)

	deployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), "image-registry", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	mem, ok := deployment.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]
	if !ok {
		framework.DumpYAML(t, "deployment", deployment)
		t.Errorf("no memory limit set on registry deployment")
	}

	if mem.String() != "512Mi" {
		t.Errorf("expected memory limit of 512Mi, found: %s", mem.String())
	}
}

func TestRolloutStrategyConfiguration(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
		Resources: &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		RolloutStrategy: string(appsapi.RecreateDeploymentStrategyType),
		NodeSelector: map[string]string{
			"node-role.kubernetes.io/master": "",
		},
		Tolerations: []corev1.Toleration{
			{
				Key:      "node-role.kubernetes.io/master",
				Operator: "Exists",
				Effect:   "NoSchedule",
			},
		},
	})
	defer framework.TeardownImageRegistry(te)

	deployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if deployment.Spec.Strategy.Type != appsapi.RecreateDeploymentStrategyType {
		t.Errorf("expected %v deployment strategy", appsapi.RecreateDeploymentStrategyType)
	}
}

func TestPodTolerationsConfiguration(t *testing.T) {
	tolerations := []corev1.Toleration{
		{
			Key:      "mykey",
			Value:    "myvalue",
			Effect:   "NoSchedule",
			Operator: "Equal",
		},
	}

	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas:    1,
		Tolerations: tolerations,
	})
	defer framework.TeardownImageRegistry(te)

	deployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), "image-registry", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(tolerations, deployment.Spec.Template.Spec.Tolerations) {
		t.Errorf("expected tolerations not found wanted: %#v, got %#v", tolerations, deployment.Spec.Template.Spec.Tolerations)
	}
}

func TestPodAffinityConfiguration(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	affinity := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "myExampleKey",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"value1", "value2"},
							},
						},
					},
				},
			},
		},
	}

	framework.DeployImageRegistry(te, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
		Affinity: affinity,
	})

	// The operator won't become available because of affinity settings. So
	// instead of waiting for the operator, we'll just hope that everything is
	// fine and the deployment will eventually be created.
	deployment, err := framework.WaitForRegistryDeployment(te.Client())
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(affinity, deployment.Spec.Template.Spec.Affinity) {
		t.Errorf("expected affinity configuration not found wanted: %#v, got %#v", affinity, deployment.Spec.Template.Spec.Affinity)
	}
}

func TestRouteConfiguration(t *testing.T) {
	hostname := "test.example.com"

	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas:     1,
		DefaultRoute: true,
		Routes: []imageregistryapiv1.ImageRegistryConfigRoute{
			{
				Name:     "testroute",
				Hostname: hostname,
			},
		},
	})
	defer framework.TeardownImageRegistry(te)

	framework.EnsureDefaultExternalRegistryHostnameIsSet(te)
	framework.EnsureExternalRegistryHostnamesAreSet(te, []string{hostname})
	framework.EnsureDefaultExternalRouteExists(te)
	framework.EnsureExternalRoutesExist(t, te.Client(), []string{hostname})
}

func TestOperatorProxyConfiguration(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)
	defer framework.ResetClusterProxyConfig(te)

	// Get the service network to set as NO_PROXY so that the
	// operator will come up once it is re-deployed
	network, err := te.Client().Networks().Get(
		context.Background(), "cluster", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("unable to get network configuration: %v", err)
	}

	// Set the proxy env vars
	t.Logf("setting fake proxy environment variables on the operator deployment...")
	if _, err := te.Client().Deployments(framework.OperatorDeploymentNamespace).Patch(
		context.Background(),
		framework.OperatorDeploymentName,
		types.StrategicMergePatchType,
		[]byte(fmt.Sprintf(`{"spec": {"template": {"spec": {"containers": [{"name":"cluster-image-registry-operator","env":[{"name":"HTTP_PROXY","value":"http://http.example.org"},{"name":"HTTPS_PROXY","value":"https://https.example.org"},{"name":"NO_PROXY","value":"%s"}]}]}}}}`, strings.Join(network.Spec.ServiceNetwork, ","))),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("failed to patch operator env vars: %v", err)
	}
	defer func() {
		t.Logf("resetting proxy environment variables of the operator deployment...")
		if _, err := te.Client().Deployments(framework.OperatorDeploymentNamespace).Patch(
			context.Background(),
			framework.OperatorDeploymentName,
			types.StrategicMergePatchType,
			framework.MarshalJSON(map[string]interface{}{
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []map[string]interface{}{
								{
									"name": "cluster-image-registry-operator",
									"env": []map[string]interface{}{
										{"name": "NO_PROXY", "$patch": "delete"},
										{"name": "HTTP_PROXY", "$patch": "delete"},
										{"name": "HTTPS_PROXY", "$patch": "delete"},
									},
								},
							},
						},
					},
				},
			}),
			metav1.PatchOptions{},
		); err != nil {
			t.Fatalf("failed to patch operator env vars: %v", err)
		}

		framework.WaitUntilDeploymentIsRolledOut(te, framework.OperatorDeploymentNamespace, framework.OperatorDeploymentName)
	}()

	// Wait for the registry operator to be re-deployed
	// after the proxy information is injected into the deployment
	framework.WaitUntilDeploymentIsRolledOut(te, framework.OperatorDeploymentNamespace, framework.OperatorDeploymentName)

	// Wait for the image registry resource to have an updated StorageExists condition
	// showing that the operator can no longer reach the storage providers api
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageExists, operatorv1.ConditionUnknown, "")

	t.Logf("resetting proxy environment variables of the operator deployment...")
	if _, err := te.Client().Deployments(framework.OperatorDeploymentNamespace).Patch(
		context.Background(),
		framework.OperatorDeploymentName,
		types.StrategicMergePatchType,
		framework.MarshalJSON(map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []map[string]interface{}{
							{
								"name": "cluster-image-registry-operator",
								"env": []map[string]interface{}{
									{"name": "NO_PROXY", "$patch": "delete"},
									{"name": "HTTP_PROXY", "$patch": "delete"},
									{"name": "HTTPS_PROXY", "$patch": "delete"},
								},
							},
						},
					},
				},
			},
		}),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("failed to patch operator env vars: %v", err)
	}

	framework.WaitUntilDeploymentIsRolledOut(te, framework.OperatorDeploymentNamespace, framework.OperatorDeploymentName)

	// Wait for the image registry resource to have an updated StorageExists condition
	// showing that operator can now reach the storage providers api
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageExists, operatorv1.ConditionTrue, "")
}

func TestOperandProxyConfiguration(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
	})
	defer framework.TeardownImageRegistry(te)

	defer framework.ResetClusterProxyConfig(te)
	defer func() {
		if t.Failed() {
			framework.DumpClusterProxyResource(te)
			framework.DumpImageRegistryDeployment(te)
		}
	}()

	resourceProxyConfig := imageregistryapiv1.ImageRegistryConfigProxy{
		NoProxy: "resourcenoproxy.example.com",
		HTTP:    "http://resourcehttpproxy.example.com",
		HTTPS:   "https://resourcehttpsproxy.example.com",
	}

	clusterProxyConfig := configapiv1.ProxySpec{
		NoProxy:    "clusternoproxy.example.com",
		HTTPProxy:  "http://clusterhttpproxy.example.com",
		HTTPSProxy: "https://clusterhttpsproxy.example.com",
	}

	resourceVars := []corev1.EnvVar{
		{Name: "NO_PROXY", Value: resourceProxyConfig.NoProxy},
		{Name: "HTTP_PROXY", Value: resourceProxyConfig.HTTP},
		{Name: "HTTPS_PROXY", Value: resourceProxyConfig.HTTPS},
	}
	clusterVars := []corev1.EnvVar{
		{Name: "NO_PROXY", Value: clusterProxyConfig.NoProxy},
		{Name: "HTTP_PROXY", Value: clusterProxyConfig.HTTPProxy},
		{Name: "HTTPS_PROXY", Value: clusterProxyConfig.HTTPSProxy},
	}

	registryDeployment := framework.GetImageRegistryDeployment(te)

	// Check that the default deployment does not contain any proxy settings
	framework.CheckEnvVarsAreNotSet(
		te,
		registryDeployment.Spec.Template.Spec.Containers[0].Env,
		[]string{"NO_PROXY", "HTTP_PROXY", "HTTPS_PROXY"},
	)

	// Patch the cluster proxy config to set the proxy settings
	framework.SetClusterProxyConfig(te, clusterProxyConfig)

	t.Logf("waiting for the operator to recreate the deployment...")
	framework.WaitUntilImageRegistryConfigIsProcessed(te)
	registryDeployment = framework.GetImageRegistryDeployment(te)

	// Check that the new deployment contains the cluster proxy settings
	framework.CheckEnvVars(te, clusterVars, registryDeployment.Spec.Template.Spec.Containers[0].Env, true)

	// Patch the image registry resource to contain the proxy settings
	framework.SetResourceProxyConfig(te, resourceProxyConfig)

	t.Logf("waiting for the operator to recreate the deployment...")
	framework.WaitUntilImageRegistryConfigIsProcessed(te)
	registryDeployment = framework.GetImageRegistryDeployment(te)

	// Check that the new deployment contains the resource proxy settings (overriding the cluster proxy config)
	framework.CheckEnvVars(te, resourceVars, registryDeployment.Spec.Template.Spec.Containers[0].Env, true)
}

func TestSecureRouteConfiguration(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	hostname := "test.example.com"
	cert, key, err := framework.GenerateX509Certificate(hostname, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	routeName := "testroute"
	tlsSecretName := "testroute-tls"
	tlsSecretData := map[string]string{
		"tls.crt": string(cert),
		"tls.key": string(key),
	}

	if _, err := framework.CreateOrUpdateSecret(context.Background(), tlsSecretName, defaults.ImageRegistryOperatorNamespace, tlsSecretData); err != nil {
		t.Fatalf("unable to create secret: %s", err)
	}

	framework.DeployImageRegistry(te, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
		Routes: []imageregistryapiv1.ImageRegistryConfigRoute{
			{
				Name:       routeName,
				Hostname:   hostname,
				SecretName: tlsSecretName,
			},
		},
	})
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)
	framework.EnsureExternalRegistryHostnamesAreSet(te, []string{hostname})

	err = wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 1*time.Minute, false,
		func(ctx context.Context) (done bool, err error) {
			route, err := te.Client().Routes(defaults.ImageRegistryOperatorNamespace).Get(
				ctx, routeName, metav1.GetOptions{},
			)
			if err != nil {
				t.Logf("unable to get route: %s", err)
				return false, nil
			}
			if route.Spec.TLS == nil {
				t.Fatal("route.Spec.TLS is nil, want a configuration")
			}
			if route.Spec.TLS.Certificate != string(cert) {
				t.Errorf("route tls certificate: got %q, want %q", route.Spec.TLS.Certificate, string(cert))
			}
			if route.Spec.TLS.Key != string(key) {
				t.Errorf("route tls key: got %q, want %q", route.Spec.TLS.Key, string(key))
			}
			return true, nil
		},
	)
	if err != nil {
		t.Fatalf("failed to observe the route: %s", err)
	}
}

func TestVersionReporting(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
	})
	defer framework.TeardownImageRegistry(te)

	if _, err := te.Client().Deployments(framework.OperatorDeploymentNamespace).Patch(
		context.Background(),
		framework.OperatorDeploymentName,
		types.StrategicMergePatchType,
		[]byte(`{"spec": {"template": {"spec": {"containers": [{"name":"cluster-image-registry-operator","env":[{"name":"RELEASE_VERSION","value":"test-v2"}]}]}}}}`),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("failed to patch operator to new version: %v", err)
	}

	framework.WaitUntilDeploymentIsRolledOut(te, framework.OperatorDeploymentNamespace, framework.OperatorDeploymentName)

	err := wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 1*time.Minute, false,
		func(ctx context.Context) (bool, error) {
			clusterOperatorStatus, err := te.Client().ClusterOperators().Get(
				ctx, defaults.ImageRegistryClusterOperatorResourceName, metav1.GetOptions{},
			)
			if err != nil {
				t.Logf("Could not retrieve cluster operator status: %v", err)
				return false, nil
			}
			if len(clusterOperatorStatus.Status.Versions) == 0 {
				// We should always have *some* version information in the clusteroperator once we are available,
				// so we do not retry in this scenario.
				t.Fatalf("Cluster operator status has no version information: %v", clusterOperatorStatus)
				return true, err
			}
			if clusterOperatorStatus.Status.Versions[0].Name != "operator" || clusterOperatorStatus.Status.Versions[0].Version != "test-v2" {
				t.Logf("waiting for new version to be reported, saw: %v", clusterOperatorStatus.Status.Versions[0])
				return false, nil
			}
			return true, nil
		},
	)
	if err != nil {
		t.Fatalf("failed to observe updated version reported in clusteroperator status: %v", err)
	}
}

func TestRequests(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Requests: imageregistryapiv1.ImageRegistryConfigRequests{
			Read: imageregistryapiv1.ImageRegistryConfigRequestsLimits{
				MaxRunning: 1,
				MaxInQueue: 2,
				MaxWaitInQueue: metav1.Duration{
					Duration: 3 * time.Second,
				},
			},
			Write: imageregistryapiv1.ImageRegistryConfigRequestsLimits{
				MaxRunning: 4,
				MaxInQueue: 5,
				MaxWaitInQueue: metav1.Duration{
					Duration: 6 * time.Hour,
				},
			},
		},
		Replicas: 1,
	})
	defer framework.TeardownImageRegistry(te)

	deploy, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	expectedEnvVars := []corev1.EnvVar{
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXRUNNING", Value: "1", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXINQUEUE", Value: "2", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_READ_MAXWAITINQUEUE", Value: "3s", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXRUNNING", Value: "4", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXINQUEUE", Value: "5", ValueFrom: nil},
		{Name: "REGISTRY_OPENSHIFT_REQUESTS_WRITE_MAXWAITINQUEUE", Value: "6h0m0s", ValueFrom: nil},
	}
	framework.CheckEnvVars(te, expectedEnvVars, deploy.Spec.Template.Spec.Containers[0].Env, false)
}

func TestDisableRedirect(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		DisableRedirect: true,
		Replicas:        1,
	})
	defer framework.TeardownImageRegistry(te)

	deploy, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	expectedEnvVars := []corev1.EnvVar{
		{Name: "REGISTRY_STORAGE_REDIRECT_DISABLE", Value: "true", ValueFrom: nil},
	}
	framework.CheckEnvVars(te, expectedEnvVars, deploy.Spec.Template.Spec.Containers[0].Env, false)
}

func TestScaleUp(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 2,
	})
	defer framework.TeardownImageRegistry(te)

	if _, err := te.Client().Configs().Patch(
		context.Background(),
		defaults.ImageRegistryResourceName,
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:    "replace",
				Path:  "/spec/replicas",
				Value: 4,
			},
		}),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("unable update spec.replicas: %s", err)
	}

	cr := framework.WaitUntilImageRegistryConfigIsProcessed(te)
	if cr.Status.ReadyReplicas != 4 {
		t.Errorf("got %d ready replicas, want 4", cr.Status.ReadyReplicas)
	}
}

func TestScaleDown(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 2,
	})
	defer framework.TeardownImageRegistry(te)

	if _, err := te.Client().Configs().Patch(
		context.Background(),
		defaults.ImageRegistryResourceName,
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:    "replace",
				Path:  "/spec/replicas",
				Value: 1,
			},
		}),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("unable update spec.replicas: %s", err)
	}

	cr := framework.WaitUntilImageRegistryConfigIsProcessed(te)
	if cr.Status.ReadyReplicas != 1 {
		t.Errorf("got %d ready replicas, want 1", cr.Status.ReadyReplicas)
	}
}
