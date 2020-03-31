package e2e

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestReadOnly(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorapi.Managed,
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
		},
		ReadOnly: true,
		Replicas: 1,
	})
	defer framework.TeardownImageRegistry(te)

	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	deploy, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, env := range deploy.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "REGISTRY_STORAGE_MAINTENANCE_READONLY" {
			if expected := "{enabled: true}"; env.Value != expected {
				t.Errorf("%s: got %q, want %q", env.Name, env.Value, expected)
			} else {
				found = true
			}
		}
	}
	if !found {
		framework.DumpObject(t, "deployment", deploy)
		t.Error("environment variable REGISTRY_STORAGE_MAINTENANCE_READONLY_ENABLED=true is not found")
	}
}
