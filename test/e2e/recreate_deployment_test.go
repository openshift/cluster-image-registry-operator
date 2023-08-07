package e2e

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestRecreateDeployment(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
	})
	defer framework.TeardownImageRegistry(te)

	t.Logf("deleting the image registry deployment...")
	if err := framework.DeleteCompletely(
		func() (metav1.Object, error) {
			return te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
				context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
			)
		},
		func(deleteOptions metav1.DeleteOptions) error {
			return te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Delete(
				context.Background(), defaults.ImageRegistryName, deleteOptions,
			)
		},
	); err != nil {
		t.Fatalf("unable to delete the deployment: %s", err)
	}

	t.Logf("waiting for the operator to recreate the deployment...")
	if _, err := framework.WaitForRegistryDeployment(te.Client()); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreDeploymentAfterUserChanges(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	// add a new environment variable and a host port to the deployment.
	if _, err := te.Client().Deployments(framework.OperatorDeploymentNamespace).Patch(
		context.Background(),
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
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("unable to patch image registry deployment: %v", err)
	}

	// wait for the Deployment to be ovewritten by the operator.
	if err := wait.PollUntilContextTimeout(context.Background(), time.Second, time.Minute, false,
		func(ctx context.Context) (stop bool, err error) {
			deployment, err := te.Client().Deployments(
				framework.OperatorDeploymentNamespace,
			).Get(ctx, "image-registry", metav1.GetOptions{})
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
