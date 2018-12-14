package testframework

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	osapi "github.com/openshift/api/config/v1"
	operatorapi "github.com/openshift/api/operator/v1alpha1"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

const (
	ImageRegistryName                = "image-registry"
	ImageRegistryDeploymentName      = ImageRegistryName
	ImageRegistryDeploymentNamespace = OperatorDeploymentNamespace
)

type ConditionStatus struct {
	status *operatorapi.ConditionStatus
}

func NewConditionStatus(cond operatorapi.OperatorCondition) ConditionStatus {
	return ConditionStatus{
		status: &cond.Status,
	}
}

func (cs ConditionStatus) String() string {
	if cs.status == nil {
		return "<unset>"
	}
	return string(*cs.status)
}

func (cs ConditionStatus) IsTrue() bool {
	return cs.status != nil && *cs.status == operatorapi.ConditionTrue
}

func (cs ConditionStatus) IsFalse() bool {
	return cs.status != nil && *cs.status == operatorapi.ConditionFalse
}

type ImageRegistryConditions struct {
	Available   ConditionStatus
	Progressing ConditionStatus
	Failing     ConditionStatus
	Removed     ConditionStatus
}

func GetImageRegistryConditions(cr *imageregistryapi.ImageRegistry) ImageRegistryConditions {
	conds := ImageRegistryConditions{}
	for _, cond := range cr.Status.Conditions {
		switch cond.Type {
		case operatorapi.OperatorStatusTypeAvailable:
			conds.Available = NewConditionStatus(cond)
		case operatorapi.OperatorStatusTypeProgressing:
			conds.Progressing = NewConditionStatus(cond)
		case operatorapi.OperatorStatusTypeFailing:
			conds.Failing = NewConditionStatus(cond)
		case imageregistryapi.OperatorStatusTypeRemoved:
			conds.Removed = NewConditionStatus(cond)
		}
	}
	return conds
}

func (c ImageRegistryConditions) String() string {
	return fmt.Sprintf(
		"available (%s), progressing (%s), failing (%s), removed (%s)",
		c.Available, c.Progressing, c.Failing, c.Removed,
	)
}

func ensureImageRegistryToBeRemoved(logger Logger, client *Clientset) error {
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

	var cr *imageregistryapi.ImageRegistry
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = client.ImageRegistries().Get(ImageRegistryName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			cr = nil
			return true, nil
		} else if err != nil {
			return false, err
		}

		conds := GetImageRegistryConditions(cr)
		logger.Logf("waiting for the registry to be removed: %s", conds)
		return conds.Progressing.IsFalse() && conds.Removed.IsTrue(), nil
	})
	if err != nil {
		DumpObject(logger, "the latest observed state of the image registry resource", cr)
		DumpOperatorLogs(logger, client)
		return fmt.Errorf("failed to wait for the imageregistry resource to be removed: %s", err)
	}
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
	if err := ensureImageRegistryToBeRemoved(logger, client); err != nil {
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

func ensureImageRegistryIsProcessed(logger Logger, client *Clientset) (*imageregistryapi.ImageRegistry, error) {
	var cr *imageregistryapi.ImageRegistry
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = client.ImageRegistries().Get(ImageRegistryName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			logger.Logf("waiting for the registry: the resource does not exist")
			cr = nil
			return false, nil
		} else if err != nil {
			return false, err
		}

		conds := GetImageRegistryConditions(cr)
		logger.Logf("waiting for the registry: %s", conds)
		return conds.Progressing.IsFalse() && conds.Available.IsTrue() || conds.Failing.IsTrue(), nil
	})
	if err != nil {
		DumpObject(logger, "the latest observed state of the image registry resource", cr)
		DumpOperatorLogs(logger, client)
		return cr, fmt.Errorf("failed to wait for the imageregistry resource to be processed: %s", err)
	}
	return cr, nil
}

func MustEnsureImageRegistryIsProcessed(t *testing.T, client *Clientset) *imageregistryapi.ImageRegistry {
	cr, err := ensureImageRegistryIsProcessed(t, client)
	if err != nil {
		t.Fatal(err)
	}
	return cr
}

func ensureImageRegistryIsAvailable(logger Logger, client *Clientset) error {
	logger.Logf("waiting the operator to deploy the registry...")

	cr, err := ensureImageRegistryIsProcessed(logger, client)
	if err != nil {
		return err
	}

	conds := GetImageRegistryConditions(cr)
	if conds.Progressing.IsTrue() || conds.Available.IsFalse() {
		DumpObject(logger, "the latest observed state of the image registry resource", cr)
		DumpOperatorLogs(logger, client)
		return fmt.Errorf("the imageregistry resource is processed, but the the image registry is not available")
	}

	logger.Logf("the image registry resource reports that the registry is deployed and available")
	return nil
}

func MustEnsureImageRegistryIsAvailable(t *testing.T, client *Clientset) {
	if err := ensureImageRegistryIsAvailable(t, client); err != nil {
		t.Fatal(err)
	}
}

func ensureInternalRegistryHostnameIsSet(logger Logger, client *Clientset) error {
	var cr *imageregistryapi.ImageRegistry
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = client.ImageRegistries().Get(ImageRegistryName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			logger.Logf("waiting for the registry: the resource does not exist")
			cr = nil
			return false, nil
		} else if err != nil {
			return false, err
		}
		if cr == nil || cr.Status.InternalRegistryHostname != "image-registry.openshift-image-registry.svc:5000" {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		logger.Logf("registry resource was not updated with internal registry hostname: %v", err)
		return err
	}

	var cfg *configv1.Image
	err = wait.Poll(1*time.Second, AsyncOperationTimeout, func() (bool, error) {
		var err error
		cfg, err = client.Images().Get("cluster", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			logger.Logf("waiting for the image config resource: the resource does not exist")
			cfg = nil
			return false, nil
		} else if err != nil {
			return false, err
		}
		if cfg == nil || cfg.Status.InternalRegistryHostname != "image-registry.openshift-image-registry.svc:5000" {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		logger.Logf("cluster image config resource was not updated with internal registry hostname: %v", err)
	}
	return err
}

func MustEnsureInternalRegistryHostnameIsSet(t *testing.T, client *Clientset) {
	if err := ensureInternalRegistryHostnameIsSet(t, client); err != nil {
		t.Fatal(err)
	}

}

func hasExpectedClusterOperatorConditions(status *configv1.ClusterOperator) bool {
	gotAvailable := false
	gotProgressing := false
	gotFailing := false
	for _, c := range status.Status.Conditions {
		if c.Type == operatorapi.OperatorStatusTypeAvailable && c.Status == osapi.ConditionTrue {
			gotAvailable = true
		}
		if c.Type == operatorapi.OperatorStatusTypeProgressing && c.Status == osapi.ConditionFalse {
			gotProgressing = true
		}
		if c.Type == operatorapi.OperatorStatusTypeFailing && c.Status == osapi.ConditionFalse {
			gotFailing = true
		}
	}
	return gotAvailable && gotProgressing && gotFailing
}

func ensureClusterOperatorStatusIsSet(logger Logger, client *Clientset) error {
	var status *configv1.ClusterOperator
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		status, err = client.ClusterOperators().Get("cluster-image-registry-operator", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			logger.Logf("waiting for the cluster operator resource: the resource does not exist")
			return false, nil
		} else if err != nil {
			return false, err
		}
		if hasExpectedClusterOperatorConditions(status) {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		logger.Logf("clusteroperator status resource was not updated with the expected status: %v", err)
		if status != nil {
			logger.Logf("clusteroperator conditions are: %#v", status.Status.Conditions)
		}
	}
	return err
}

func MustEnsureClusterOperatorStatusIsSet(t *testing.T, client *Clientset) {
	if err := ensureClusterOperatorStatusIsSet(t, client); err != nil {
		t.Fatal(err)
	}
}
