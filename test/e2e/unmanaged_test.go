package e2e_test

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapi "github.com/openshift/api/operator/v1alpha1"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/testframework"
)

func TestUnmanaged(t *testing.T) {
	client := testframework.MustNewClientset(t, nil)

	defer testframework.MustRemoveImageRegistry(t, client)

	testframework.MustDeployImageRegistry(t, client, &imageregistryapi.ImageRegistry{
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
	})
	testframework.MustEnsureImageRegistryIsAvailable(t, client)

	cr, err := client.ImageRegistries().Get(testframework.ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	cr.Spec.ManagementState = operatorapi.Unmanaged

	cr, err = client.ImageRegistries().Update(cr)
	if err != nil {
		t.Fatal(err)
	}

	// TODO(dmage): wait for the resource to be observed

	err = client.Deployments(testframework.ImageRegistryDeploymentNamespace).Delete(testframework.ImageRegistryDeploymentName, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}

	err = wait.Poll(1*time.Second, testframework.AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = client.ImageRegistries().Get(testframework.ImageRegistryName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		conds := testframework.GetImageRegistryConditions(cr)
		t.Logf("image registry: %s", conds)
		return conds.Available.IsFalse() && conds.Progressing.IsFalse(), err
	})
	if err != nil {
		testframework.DumpImageRegistryResource(t, client)
		testframework.DumpOperatorLogs(t, client)
		t.Fatal(err)
	}
}
