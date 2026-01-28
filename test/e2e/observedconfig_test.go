package e2e

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	imageregistryapiv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

// TestObservedConfigTLSSecurityProfile verifies that the image registry
// operator correctly populates Config.Spec.ObservedConfig with TLS settings
// from the cluster's APIServer configuration.
func TestObservedConfigTLSSecurityProfile(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryapiv1.ImageRegistrySpec{
		Replicas: 1,
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryapiv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
		},
	})
	defer framework.TeardownImageRegistry(te)

	ctx := context.Background()
	config, err := te.Client().Configs().Get(ctx, defaults.ImageRegistryResourceName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(config.Spec.ObservedConfig.Raw) == 0 {
		t.Fatal("expected ObservedConfig to be populated, but it was empty")
	}

	observedConfig := map[string]any{}
	if err := json.Unmarshal(config.Spec.ObservedConfig.Raw, &observedConfig); err != nil {
		t.Fatalf("failed to unmarshal ObservedConfig: %v", err)
	}

	_, found, err := unstructured.NestedMap(observedConfig, "servingInfo")
	if err != nil {
		t.Fatalf("failed to get servingInfo from observedConfig: %v", err)
	}
	if !found {
		t.Errorf("expected servingInfo in ObservedConfig")
		framework.DumpYAML(t, "observedConfig", observedConfig)
		return
	}

	minTLSVersion, found, err := unstructured.NestedString(observedConfig, "servingInfo", "minTLSVersion")
	if err != nil {
		t.Fatalf("failed to get servingInfo.minTLSVersion: %v", err)
	}
	if !found || minTLSVersion == "" {
		t.Errorf("expected minTLSVersion in servingInfo")
	}

	cipherSuites, found, err := unstructured.NestedStringSlice(observedConfig, "servingInfo", "cipherSuites")
	if err != nil {
		t.Fatalf("failed to get servingInfo.cipherSuites: %v", err)
	}
	if !found || len(cipherSuites) == 0 {
		t.Errorf("expected cipherSuites in servingInfo")
	}

	deployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		ctx, defaults.ImageRegistryName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("deployment has no containers")
	}
	containerEnv := deployment.Spec.Template.Spec.Containers[0].Env

	var foundMinTLSEnv bool
	for _, env := range containerEnv {
		if env.Name != "REGISTRY_HTTP_TLS_MINVERSION" {
			continue
		}

		foundMinTLSEnv = true
		if env.Value != minTLSVersion {
			t.Errorf("expected REGISTRY_HTTP_TLS_MINVERSION=%s, got %s", minTLSVersion, env.Value)
		}
		break
	}
	if !foundMinTLSEnv {
		t.Error("expected REGISTRY_HTTP_TLS_MINVERSION to be set in deployment")
	}

	var foundCipherSuitesEnv bool
	for _, env := range containerEnv {
		if env.Name != "OPENSHIFT_REGISTRY_HTTP_TLS_CIPHERSUITES" {
			continue
		}
		foundCipherSuitesEnv = true
		if env.Value == "" {
			t.Error("expected OPENSHIFT_REGISTRY_HTTP_TLS_CIPHERSUITES to have a value")
		}
		break
	}
	if !foundCipherSuitesEnv {
		t.Error("expected OPENSHIFT_REGISTRY_HTTP_TLS_CIPHERSUITES to be set in deployment")
	}
}
