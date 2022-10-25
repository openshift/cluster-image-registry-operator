package e2e

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestManagementStateUnmanaged(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	if _, err := te.Client().Configs().Patch(
		context.Background(),
		defaults.ImageRegistryResourceName,
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:    "replace",
				Path:  "/spec/managementState",
				Value: operatorv1.Unmanaged,
			},
		}),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("unable to switch to unmanaged state: %s", err)
	}

	err := wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err := te.Client().Configs().Get(
			context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
		)
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
		context.Background(),
		defaults.ImageRegistryResourceName,
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:    "replace",
				Path:  "/spec/managementState",
				Value: operatorv1.Removed,
			},
		}),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("unable to switch to removed state: %s", err)
	}

	err := wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err := te.Client().Configs().Get(
			context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
		)
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

	d, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
	)
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
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Removed,
		},
		Replicas: 1,
	})

	t.Log("make sure operator is reporting itself as Removed")
	err = wait.Poll(
		time.Second,
		framework.AsyncOperationTimeout,
		func() (stop bool, err error) {
			cr, err = te.Client().Configs().Get(
				context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
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
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if cr, err = te.Client().Configs().Get(
			context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
		); err != nil {
			return err
		}

		cr.Spec.ManagementState = operatorv1.Managed
		cr.Spec.Storage = imageregistryv1.ImageRegistryConfigStorage{}

		_, err = te.Client().Configs().Update(context.Background(), cr, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Log("making sure image registry is up and running")
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)
}

func TestStorageManagementState(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	cr, err := te.Client().Configs().Get(
		context.Background(), "cluster", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("error getting config: %v", err)
	}

	if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged {
		t.Errorf(
			"Spec.Storage.ManagementState not 'Managed', %q instead",
			cr.Spec.Storage.ManagementState,
		)
	}
	if cr.Status.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged {
		t.Errorf(
			"Status.Storage.ManagementState not 'Managed', %q instead",
			cr.Status.Storage.ManagementState,
		)
	}
	if !cr.Status.StorageManaged {
		t.Errorf("Status.StorageManaged not set")
	}

	cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateUnmanaged
	if _, err := te.Client().Configs().Update(
		context.Background(), cr, metav1.UpdateOptions{},
	); err != nil {
		t.Errorf("error updating config: %v", err)
	}

	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)

	cr, err = te.Client().Configs().Get(
		context.Background(), "cluster", metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("error getting config: %v", err)
	}

	if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateUnmanaged {
		t.Errorf(
			"Spec.Storage.ManagementState not 'Unmanaged', %q instead",
			cr.Spec.Storage.ManagementState,
		)
	}
	if cr.Status.Storage.ManagementState != imageregistryv1.StorageManagementStateUnmanaged {
		t.Errorf(
			"Status.Storage.ManagementState not 'Unmanaged', %q instead",
			cr.Status.Storage.ManagementState,
		)
	}
	if cr.Status.StorageManaged {
		t.Errorf("Status.StorageManaged set")
	}
}
