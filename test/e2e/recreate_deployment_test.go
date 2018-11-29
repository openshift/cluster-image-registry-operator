package e2e_test

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapi "github.com/openshift/api/operator/v1alpha1"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/testframework"
)

func TestRecreateDeployment(t *testing.T) {
	client := testframework.MustNewClientset(t, nil)

	defer testframework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryapi.ImageRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapi.SchemeGroupVersion.String(),
			Kind:       "ImageRegistry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: testframework.ImageRegistryName,
		},
		Spec: imageregistryapi.ImageRegistrySpec{
			OperatorSpec: operatorapi.OperatorSpec{
				ManagementState: operatorapi.Managed,
			},
			Storage: imageregistryapi.ImageRegistryConfigStorage{
				Filesystem: &imageregistryapi.ImageRegistryConfigStorageFilesystem{
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			Replicas: 1,
		},
	}
	testframework.MustDeployImageRegistry(t, client, cr)
	testframework.MustEnsureImageRegistryIsAvailable(t, client)

	t.Logf("deleting the image registry deployment...")
	if err := testframework.DeleteCompletely(
		func() (metav1.Object, error) {
			return client.Deployments(testframework.ImageRegistryDeploymentNamespace).Get(testframework.ImageRegistryDeploymentName, metav1.GetOptions{})
		},
		func(deleteOptions *metav1.DeleteOptions) error {
			return client.Deployments(testframework.ImageRegistryDeploymentNamespace).Delete(testframework.ImageRegistryDeploymentName, deleteOptions)
		},
	); err != nil {
		t.Fatalf("unable to delete the deployment: %s", err)
	}

	t.Logf("waiting the operator to recreate the deployment...")
	err := wait.Poll(1*time.Second, testframework.AsyncOperationTimeout, func() (stop bool, err error) {
		_, err = client.Deployments(testframework.ImageRegistryDeploymentNamespace).Get(testframework.ImageRegistryDeploymentName, metav1.GetOptions{})
		if err == nil {
			return true, nil
		}
		t.Logf("get deployment: %s", err)
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	})
	if err != nil {
		testframework.DumpImageRegistryResource(t, client)
		testframework.DumpOperatorLogs(t, client)
		t.Fatal(err)
	}
}
