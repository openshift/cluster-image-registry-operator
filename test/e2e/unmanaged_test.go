package e2e_test

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/client-go/util/retry"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/testframework"
)

func TestUnmanaged(t *testing.T) {
	client := testframework.MustNewClientset(t, nil)

	defer testframework.MustRemoveImageRegistry(t, client)

	testframework.MustDeployImageRegistry(t, client, &imageregistryv1.Config{
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
	})
	testframework.MustEnsureImageRegistryIsAvailable(t, client)

	var cr *imageregistryv1.Config
	var err error
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cr, err = client.Configs().Get(imageregistryv1.ImageRegistryResourceName, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}

		cr.Spec.ManagementState = operatorapi.Unmanaged

		cr, err = client.Configs().Update(cr)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// TODO(dmage): wait for the resource to be observed

	err = client.Deployments(imageregistryv1.ImageRegistryOperatorNamespace).Delete(imageregistryv1.ImageRegistryName, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}

	err = wait.Poll(1*time.Second, testframework.AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = client.Configs().Get(imageregistryv1.ImageRegistryResourceName, metav1.GetOptions{})
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
