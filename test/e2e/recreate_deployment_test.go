package e2e

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
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
			Name: defaults.ImageRegistryResourceName,
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
			return client.Deployments(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
		},
		func(deleteOptions *metav1.DeleteOptions) error {
			return client.Deployments(defaults.ImageRegistryOperatorNamespace).Delete(defaults.ImageRegistryName, deleteOptions)
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

func TestRestoreDeploymentAfterUserChanges(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)

	// add a new environment variable and a host port to the deployment.
	if _, err := client.Deployments(framework.OperatorDeploymentNamespace).Patch(
		defaults.ImageRegistryName,
		types.JSONPatchType,
		[]byte(`[
			{
				"op": "add",
				"path": "/spec/template/spec/containers/0/env/-",
				"value": {"name": "FOO", "value": "BAR"}
			},
			{
				"op": "add",
				"path": "/spec/template/spec/containers/0/ports/-",
				"value": {"name": "foo", "containerPort": 2222}
			}
		]`),
	); err != nil {
		t.Fatalf("unable to patch image registry deployment: %v", err)
	}

	// wait for the Deployment to be ovewritten by the operator.
	if err := wait.Poll(
		time.Second,
		time.Minute,
		func() (stop bool, err error) {
			deployment, err := client.Deployments(
				framework.OperatorDeploymentNamespace,
			).Get("image-registry", metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			// new environment variable should have been vanished.
			for _, env := range deployment.Spec.Template.Spec.Containers[0].Env {
				if env.Name == "FOO" {
					return false, nil
				}
			}

			// new host port should have been vanished.
			for _, port := range deployment.Spec.Template.Spec.Containers[0].Ports {
				if port.Name == "foo" {
					return false, nil
				}
			}

			return true, nil
		},
	); err != nil {
		t.Errorf("registry deployment not retored: %v", err)
	}
}
