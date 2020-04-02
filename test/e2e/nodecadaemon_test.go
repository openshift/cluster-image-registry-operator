package e2e

import (
	"context"
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
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	framework.DeployImageRegistry(te, &imageregistryapiv1.ImageRegistrySpec{
		ManagementState: operatorapiv1.Removed,
		Replicas:        1,
	})
	framework.WaitUntilImageRegistryIsAvailable(te)

	t.Log("waiting until the node-ca daemon is deployed")
	err := wait.Poll(5*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		ds, err := te.Client().DaemonSets(defaults.ImageRegistryOperatorNamespace).Get(
			context.Background(), "node-ca", metav1.GetOptions{},
		)
		if errors.IsNotFound(err) {
			t.Logf("ds/node-ca has not been created yet: %s", err)
			return false, nil
		} else if err != nil {
			return false, err
		}

		if ds.Status.NumberAvailable == 0 {
			t.Logf("ds/node-ca has no available replicas")
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
