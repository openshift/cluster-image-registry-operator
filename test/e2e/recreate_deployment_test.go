package e2e_test

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/testframework"
)

func TestRecreateDeployment(t *testing.T) {
	client := testframework.MustNewClientset(t, nil)

	defer testframework.MustRemoveImageRegistry(t, client)

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
				Filesystem: &imageregistryv1.ImageRegistryConfigStorageFilesystem{
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
			return client.Deployments(imageregistryv1.ImageRegistryOperatorNamespace).Get(imageregistryv1.ImageRegistryName, metav1.GetOptions{})
		},
		func(deleteOptions *metav1.DeleteOptions) error {
			return client.Deployments(imageregistryv1.ImageRegistryOperatorNamespace).Delete(imageregistryv1.ImageRegistryName, deleteOptions)
		},
	); err != nil {
		t.Fatalf("unable to delete the deployment: %s", err)
	}

	t.Logf("waiting the operator to recreate the deployment...")
	err := wait.Poll(1*time.Second, testframework.AsyncOperationTimeout, func() (stop bool, err error) {
		_, err = client.Deployments(imageregistryv1.ImageRegistryOperatorNamespace).Get(imageregistryv1.ImageRegistryName, metav1.GetOptions{})
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
