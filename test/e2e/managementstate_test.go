package e2e

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestManagementStateUnmanaged(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	if _, err := te.Client().Configs().Patch(
		defaults.ImageRegistryResourceName,
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:    "replace",
				Path:  "/spec/managementState",
				Value: operatorapi.Unmanaged,
			},
		}),
	); err != nil {
		t.Fatalf("unable to switch to unmanaged state: %s", err)
	}

	err := wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err := te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		conds := framework.GetImageRegistryConditions(cr)
		t.Logf("image registry: %s", conds)
		return conds.Available.IsTrue() && conds.Available.Reason() == "Unmanaged" &&
			conds.Progressing.IsFalse() && conds.Progressing.Reason() == "Unmanaged" &&
			conds.Degraded.IsFalse() && conds.Degraded.Reason() == "Unmanaged", nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestManagementStateRemoved(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	if _, err := te.Client().Configs().Patch(
		defaults.ImageRegistryResourceName,
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:    "replace",
				Path:  "/spec/managementState",
				Value: operatorapi.Removed,
			},
		}),
	); err != nil {
		t.Fatalf("unable to switch to removed state: %s", err)
	}

	err := wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err := te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		conds := framework.GetImageRegistryConditions(cr)
		t.Logf("image registry: %s", conds)
		return conds.Available.IsTrue() && conds.Available.Reason() == "Removed" &&
			conds.Progressing.IsFalse() && conds.Progressing.Reason() == "Removed" &&
			conds.Degraded.IsFalse() &&
			conds.Removed.IsTrue(), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	d, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Fatalf("deployment is expected to be removed, got %v %v", d, err)
	}
}

func TestRemovedToManagedTransition(t *testing.T) {
	var cr *imageregistryv1.Config
	var err error

	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	if !framework.PlatformHasDefaultStorage(te) {
		t.Skip("skipping because the current platform does not provide default storage configuration")
	}

	t.Log("creating config with ManagementState set to Removed")
	framework.DeployImageRegistry(te, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorapi.Removed,
		Replicas:        1,
	})

	t.Log("make sure operator is reporting itself as Removed")
	err = wait.Poll(
		time.Second,
		framework.AsyncOperationTimeout,
		func() (stop bool, err error) {
			cr, err = te.Client().Configs().Get(
				defaults.ImageRegistryResourceName,
				metav1.GetOptions{},
			)
			if err != nil {
				return false, err
			}

			conds := framework.GetImageRegistryConditions(cr)
			return conds.Removed.IsTrue(), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("updating ManagementState to Managed with no storage config")
	cr.Spec.ManagementState = operatorapi.Managed
	cr.Spec.Storage = imageregistryv1.ImageRegistryConfigStorage{}
	if _, err = te.Client().Configs().Update(cr); err != nil {
		t.Fatal(err)
	}

	t.Log("making sure image registry is up and running")
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)
}
