package e2e

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestRecreateDeployment(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryv1.ImageRegistryResourceName,
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			ManagementState: operatorapi.Managed,
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas: 1,
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)

	t.Logf("deleting the image registry deployment...")
	if err := framework.DeleteCompletely(
		func() (metav1.Object, error) {
			return client.Deployments(imageregistryv1.ImageRegistryOperatorNamespace).Get(imageregistryv1.ImageRegistryName, metav1.GetOptions{})
		},
		func(deleteOptions *metav1.DeleteOptions) error {
			return client.Deployments(imageregistryv1.ImageRegistryOperatorNamespace).Delete(imageregistryv1.ImageRegistryName, deleteOptions)
		},
	); err != nil {
		t.Fatalf("unable to delete the deployment: %s", err)
	}

	t.Logf("waiting for the operator to recreate the deployment...")
	if _, err := framework.WaitForRegistryDeployment(client); err != nil {
		framework.DumpImageRegistryResource(t, client)
		framework.DumpOperatorLogs(t, client)
		t.Fatal(err)
	}
}
