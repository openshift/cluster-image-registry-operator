package framework

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	configapiv1 "github.com/openshift/api/config/v1"
	osapi "github.com/openshift/api/config/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"

	imageregistryapiv1 "github.com/openshift/api/imageregistry/v1"
)

const (
	OperatorDeploymentNamespace = "openshift-image-registry"
	OperatorDeploymentName      = "cluster-image-registry-operator"
)

type ConditionStatus struct {
	status *operatorapiv1.ConditionStatus
	reason string
}

func NewConditionStatus(cond operatorapiv1.OperatorCondition) ConditionStatus {
	return ConditionStatus{
		status: &cond.Status,
		reason: cond.Reason,
	}
}

func (cs ConditionStatus) String() string {
	if cs.status == nil {
		return "<unset>"
	}
	return string(*cs.status)
}

func (cs ConditionStatus) IsTrue() bool {
	return cs.status != nil && *cs.status == operatorapiv1.ConditionTrue
}

func (cs ConditionStatus) IsFalse() bool {
	return cs.status != nil && *cs.status == operatorapiv1.ConditionFalse
}

func (cs ConditionStatus) Reason() string {
	return cs.reason
}

type ImageRegistryConditions struct {
	Available   ConditionStatus
	Progressing ConditionStatus
	Degraded    ConditionStatus
	Removed     ConditionStatus
}

func GetImageRegistryConditions(cr *imageregistryapiv1.Config) ImageRegistryConditions {
	conds := ImageRegistryConditions{}
	for _, cond := range cr.Status.Conditions {
		switch cond.Type {
		case operatorapiv1.OperatorStatusTypeAvailable:
			conds.Available = NewConditionStatus(cond)
		case operatorapiv1.OperatorStatusTypeProgressing:
			conds.Progressing = NewConditionStatus(cond)
		case operatorapiv1.OperatorStatusTypeDegraded:
			conds.Degraded = NewConditionStatus(cond)
		case defaults.OperatorStatusTypeRemoved:
			conds.Removed = NewConditionStatus(cond)
		}
	}
	return conds
}

func (c ImageRegistryConditions) String() string {
	return fmt.Sprintf(
		"available (%s), progressing (%s), Degraded (%s), removed (%s)",
		c.Available, c.Progressing, c.Degraded, c.Removed,
	)
}

func ensureImageRegistryToBeRemoved(logger Logger, client *Clientset) error {
	if _, err := client.Configs().Patch(defaults.ImageRegistryResourceName, types.MergePatchType, []byte(`{"spec": {"managementState": "Removed"}}`)); err != nil {
		if errors.IsNotFound(err) {
			// That's not exactly what we are asked for. And few seconds later
			// the operator may bootstrap it. However, if the operator is
			// disabled, it means the registry is not installed and we're
			// already in the desired state.
			return nil
		}
		return err
	}

	var cr *imageregistryapiv1.Config
	err := wait.Poll(5*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
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
		DumpYAML(logger, "the latest observed state of the image registry resource", cr)
		DumpOperatorLogs(logger, client)
		return fmt.Errorf("failed to wait for the imageregistry resource to be removed: %s", err)
	}
	return nil
}

func deleteImageRegistryResource(client *Clientset) error {
	// TODO(dmage): the finalizer should be removed by the operator
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cr, err := client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		} else if err != nil {
			return err
		}
		cr.Finalizers = nil
		_, err = client.Configs().Update(cr)
		return err
	}); err != nil {
		return err
	}

	if err := DeleteCompletely(
		func() (metav1.Object, error) {
			return client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		},
		func(deleteOptions *metav1.DeleteOptions) error {
			return client.Configs().Delete(defaults.ImageRegistryResourceName, deleteOptions)
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
	if err := StopDeployment(logger, client, OperatorDeploymentName, OperatorDeploymentNamespace); err != nil {
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

func DeployImageRegistry(logger Logger, client *Clientset, cr *imageregistryapiv1.Config) error {
	if cr != nil {
		logger.Logf("creating the image registry resource...")
		if _, err := client.Configs().Create(cr); err != nil {
			return fmt.Errorf("unable to create the image registry resource: %s", err)
		}
	}
	logger.Logf("starting the operator...")
	if err := startOperator(client); err != nil {
		return fmt.Errorf("unable to start the operator: %s", err)
	}
	return nil
}

func MustDeployImageRegistry(t *testing.T, client *Clientset, cr *imageregistryapiv1.Config) {
	if err := DeployImageRegistry(t, client, cr); err != nil {
		t.Fatal(err)
	}
}

func DumpImageRegistryResource(logger Logger, client *Clientset) {
	cr, err := client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
	if err != nil {
		logger.Logf("unable to dump the image registry resource: %s", err)
		return
	}
	DumpYAML(logger, "the image registry resource", cr)
}

func DumpImageRegistryDeployment(logger Logger, client *Clientset) {
	d, err := client.Deployments(OperatorDeploymentNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		logger.Logf("unable to dump the image registry deployment: %s", err)
		return
	}
	DumpYAML(logger, "the image registry deployment", d)
}

func ensureImageRegistryIsProcessed(logger Logger, client *Clientset) (*imageregistryapiv1.Config, error) {
	var cr *imageregistryapiv1.Config
	err := wait.Poll(5*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			logger.Logf("waiting for the registry: the resource does not exist")
			cr = nil
			return false, nil
		} else if err != nil {
			return false, err
		}

		conds := GetImageRegistryConditions(cr)
		logger.Logf("waiting for the registry: %s", conds)
		return conds.Progressing.IsFalse() && conds.Available.IsTrue() || conds.Degraded.IsTrue(), nil
	})
	if err != nil {
		DumpYAML(logger, "the latest observed state of the image registry resource", cr)
		DumpOperatorLogs(logger, client)
		return cr, fmt.Errorf("failed to wait for the imageregistry resource to be processed: %s", err)
	}
	return cr, nil
}

func MustEnsureImageRegistryIsProcessed(t *testing.T, client *Clientset) *imageregistryapiv1.Config {
	cr, err := ensureImageRegistryIsProcessed(t, client)
	if err != nil {
		t.Fatal(err)
	}
	return cr
}

func ensureImageRegistryIsAvailable(logger Logger, client *Clientset) error {
	logger.Logf("waiting for the operator to deploy the registry...")

	cr, err := ensureImageRegistryIsProcessed(logger, client)
	if err != nil {
		return err
	}

	conds := GetImageRegistryConditions(cr)
	if conds.Progressing.IsTrue() || conds.Available.IsFalse() {
		DumpYAML(logger, "the latest observed state of the image registry resource", cr)
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
	var cfg *configapiv1.Image
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (bool, error) {
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

func hasExpectedClusterOperatorConditions(status *configapiv1.ClusterOperator) bool {
	gotAvailable := false
	gotProgressing := false
	gotDegraded := false
	for _, c := range status.Status.Conditions {
		if c.Type == osapi.OperatorAvailable {
			gotAvailable = true
		}
		if c.Type == osapi.OperatorProgressing {
			gotProgressing = true
		}
		if c.Type == osapi.OperatorDegraded {
			gotDegraded = true
		}
	}
	return gotAvailable && gotProgressing && gotDegraded
}

func ensureClusterOperatorStatusIsSet(logger Logger, client *Clientset) (*configapiv1.ClusterOperator, error) {
	var status *configapiv1.ClusterOperator
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		status, err = client.ClusterOperators().Get(defaults.ImageRegistryClusterOperatorResourceName, metav1.GetOptions{})
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
	return status, err
}

func MustEnsureClusterOperatorStatusIsSet(t *testing.T, client *Clientset) *configapiv1.ClusterOperator {
	clusterOperator, err := ensureClusterOperatorStatusIsSet(t, client)
	if err != nil {
		t.Fatal(err)
	}
	return clusterOperator
}

func MustEnsureClusterOperatorStatusIsNormal(t *testing.T, client *Clientset) {
	clusterOperator := MustEnsureClusterOperatorStatusIsSet(t, client)

	for _, cond := range clusterOperator.Status.Conditions {
		switch cond.Type {
		case configapiv1.OperatorAvailable:
			if cond.Status != configapiv1.ConditionTrue {
				t.Errorf("Expected clusteroperator Available=%s, got %s", configapiv1.ConditionTrue, cond.Status)
			}
		case configapiv1.OperatorProgressing:
			if cond.Status != configapiv1.ConditionFalse {
				t.Errorf("Expected clusteroperator Progressing=%s, got %s", configapiv1.ConditionFalse, cond.Status)
			}
		case configapiv1.OperatorDegraded:
			if cond.Status != configapiv1.ConditionFalse {
				t.Errorf("Expected clusteroperator Degraded=%s, got %s", configapiv1.ConditionFalse, cond.Status)
			}
		}
	}

	namespaceFound := false
	for _, obj := range clusterOperator.Status.RelatedObjects {
		if strings.ToLower(obj.Resource) == "namespaces" {
			namespaceFound = true
			if obj.Name != defaults.ImageRegistryOperatorNamespace {
				t.Errorf("expected related namespaces resource to have name %q, got %q", defaults.ImageRegistryOperatorNamespace, obj.Name)
			}
		}
	}
	if !namespaceFound {
		t.Error("could not find related object namespaces")
	}
}

func MustEnsureOperatorIsNotHotLooping(t *testing.T, client *Clientset) {
	// Allow the operator a few seconds to stabilize
	time.Sleep(15 * time.Second)
	var cfg *imageregistryapiv1.Config
	var err error
	err = wait.Poll(1*time.Second, 30*time.Second, func() (stop bool, err error) {
		cfg, err = client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if err != nil || cfg == nil {
			t.Logf("failed to retrieve registry operator config: %v", err)
			return false, nil
		}
		return true, nil
	})
	if cfg == nil || err != nil {
		t.Errorf("failed to retrieve registry operator config: %v", err)
	}
	oldVersion := cfg.ResourceVersion

	// wait 15s and then ensure that ResourceVersion is not updated. If it was updated then something
	// is updating the registry config resource when we should be at steady state.
	time.Sleep(15 * time.Second)
	err = wait.Poll(1*time.Second, 30*time.Second, func() (stop bool, err error) {
		cfg, err = client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if err != nil || cfg == nil {
			t.Logf("failed to retrieve registry operator config: %v", err)
			return false, nil
		}
		return true, nil
	})
	if cfg == nil || err != nil {
		t.Errorf("failed to retrieve registry operator config: %v", err)
	}
	if oldVersion != cfg.ResourceVersion {
		t.Errorf("registry config resource version was updated when it should have been stable, went from %s to %s", oldVersion, cfg.ResourceVersion)
	}
}
