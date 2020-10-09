package e2e

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestNodeCADaemonAlwaysDeployed(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	framework.DeployImageRegistry(te, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorv1.Removed,
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

func TestNodeCADaemonChangesReverted(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorv1.Managed,
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
	})
	defer framework.TeardownImageRegistry(te)

	t.Log("waiting until the node-ca daemonset is created")
	err := wait.Poll(5*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		_, err = te.Client().DaemonSets(defaults.ImageRegistryOperatorNamespace).Get(
			context.Background(), "node-ca", metav1.GetOptions{},
		)
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

	t.Log("changing the node-ca daemonset")
	if _, err := te.Client().DaemonSets(defaults.ImageRegistryOperatorNamespace).Patch(
		context.Background(),
		"node-ca",
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:   "remove",
				Path: "/spec/template/spec/tolerations",
			},
		}),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("unable to remove tolerations from node-ca: %s", err)
	}

	if err := wait.Poll(time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		ds, err := te.Client().DaemonSets(defaults.ImageRegistryOperatorNamespace).Get(
			context.Background(), "node-ca", metav1.GetOptions{},
		)
		if err != nil {
			return false, fmt.Errorf("unable to get node-ca: %s", err)
		}

		t.Logf("node-ca tolerations: %#v", ds.Spec.Template.Spec.Tolerations)

		return reflect.DeepEqual(
			ds.Spec.Template.Spec.Tolerations,
			[]corev1.Toleration{
				{Operator: corev1.TolerationOpExists},
			},
		), nil
	}); err != nil {
		t.Fatalf("failed to wait until node-ca is restored: %s", err)
	}
}
