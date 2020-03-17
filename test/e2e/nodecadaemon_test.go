package e2e

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	imageregistryapiv1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestNodeCADaemonAlwaysDeployed(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryapiv1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaults.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Removed,
			Replicas:        1,
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)

	t.Log("waiting until the node-ca daemon is deployed")
	err := wait.Poll(time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		_, err = client.DaemonSets(defaults.ImageRegistryOperatorNamespace).Get("node-ca", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			t.Logf("ds/node-ca has not been created yet: %s", err)
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
