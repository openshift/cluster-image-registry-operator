package testframework

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapi "github.com/openshift/api/operator/v1alpha1"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

const (
	ImageRegistryName                = "image-registry"
	ImageRegistryDeploymentName      = ImageRegistryName
	ImageRegistryDeploymentNamespace = OperatorDeploymentNamespace
)

func ensureImageRegistryToBeRemoved(client *Clientset) error {
	if _, err := client.ImageRegistries().Patch(ImageRegistryName, types.MergePatchType, []byte(`{"spec": {"managementState": "Removed"}}`)); err != nil {
		if errors.IsNotFound(err) {
			// That's not exactly what we are asked for. And few seconds later
			// the operator may bootstrap it. However, if the operator is
			// disabled, it means the registry is not installed and we're
			// already in the desired state.
			return nil
		}
		return err
	}

	// TODO(dmage): when we have the Removed condition, this code will need to
	// be updated.
	time.Sleep(2 * time.Second)
	return nil
}

func deleteImageRegistryResource(client *Clientset) error {
	// TODO(dmage): the finalizer should be removed by the operator
	cr, err := client.ImageRegistries().Get(ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	cr.Finalizers = nil
	if _, err := client.ImageRegistries().Update(cr); err != nil {
		return err
	}

	if err := DeleteCompletely(
		func() (metav1.Object, error) {
			return client.ImageRegistries().Get(ImageRegistryName, metav1.GetOptions{})
		},
		func(deleteOptions *metav1.DeleteOptions) error {
			return client.ImageRegistries().Delete(ImageRegistryName, deleteOptions)
		},
	); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func RemoveImageRegistry(logger Logger, client *Clientset) error {
	logger.Logf("uninstalling the image registry...")
	if err := ensureImageRegistryToBeRemoved(client); err != nil {
		return fmt.Errorf("unable to uninstall the image registry: %s", err)
	}
	logger.Logf("stopping the operator...")
	if err := stopOperator(client); err != nil {
		return fmt.Errorf("unable to stop the operator: %s", err)
	}
	logger.Logf("deleting the image registry resource...")
	if err := deleteImageRegistryResource(client); err != nil {
		return fmt.Errorf("unable to delete the image registry resource: %s", err)
	}
	return nil
}

func MustRemoveImageRegistry(t *testing.T, client *Clientset) {
	if err := RemoveImageRegistry(t, client); err != nil {
		t.Fatal(err)
	}
}

func DeployImageRegistry(logger Logger, client *Clientset, cr *imageregistryapi.ImageRegistry) error {
	if cr != nil {
		logger.Logf("creating the image registry resource...")
		if _, err := client.ImageRegistries().Create(cr); err != nil {
			return fmt.Errorf("unable to create the image registry resource: %s", err)
		}
	}
	logger.Logf("starting the operator...")
	if err := startOperator(client); err != nil {
		return fmt.Errorf("unable to start the operator: %s", err)
	}
	return nil
}

func MustDeployImageRegistry(t *testing.T, client *Clientset, cr *imageregistryapi.ImageRegistry) {
	if err := DeployImageRegistry(t, client, cr); err != nil {
		t.Fatal(err)
	}
}

func DumpImageRegistryResource(logger Logger, client *Clientset) {
	cr, err := client.ImageRegistries().Get(ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		logger.Logf("unable to dump the image registry resource: %s", err)
		return
	}
	DumpObject(logger, "the image registry resource", cr)
}

func ensureImageRegistryIsAvailable(logger Logger, client *Clientset) error {
	logger.Logf("waiting the operator to deploy the registry...")
	var cr *imageregistryapi.ImageRegistry
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = client.ImageRegistries().Get(ImageRegistryName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			logger.Logf("waiting for the registry: the resource does not exist")
			return false, nil
		} else if err != nil {
			return false, err
		}

		available := false
		done := false
		for _, cond := range cr.Status.Conditions {
			switch cond.Type {
			case operatorapi.OperatorStatusTypeAvailable:
				available = cond.Status == operatorapi.ConditionTrue
			case operatorapi.OperatorStatusTypeProgressing:
				done = cond.Status == operatorapi.ConditionFalse
			}
		}
		logger.Logf("waiting for the registry: available (%t), done (%t)", available, done)

		return done && available, nil
	})
	if err != nil {
		DumpObject(logger, "the latest observed state of the image registry resource", cr)
		DumpOperatorLogs(logger, client)
		return fmt.Errorf("failed to wait for the image registry to be deployed: %s", err)
	}
	logger.Logf("the image registry resource reports that the registry is deployed and available")
	return nil
}

func MustEnsureImageRegistryIsAvailable(t *testing.T, client *Clientset) {
	if err := ensureImageRegistryIsAvailable(t, client); err != nil {
		t.Fatal(err)
	}
}
