package e2e

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/client-go/util/retry"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestUnmanaged(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	framework.MustDeployImageRegistry(t, client, &imageregistryv1.Config{
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
	})
	framework.MustEnsureImageRegistryIsAvailable(t, client)

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

	err = wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = client.Configs().Get(imageregistryv1.ImageRegistryResourceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		conds := framework.GetImageRegistryConditions(cr)
		t.Logf("image registry: %s", conds)
		return conds.Available.IsFalse() && conds.Progressing.IsFalse(), err
	})
	if err != nil {
		framework.DumpImageRegistryResource(t, client)
		framework.DumpOperatorLogs(t, client)
		t.Fatal(err)
	}
}
