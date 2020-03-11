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
	imageregistryapiv1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
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

func ensureImageRegistryToBeRemoved(te TestEnv) {
	if _, err := te.Client().Configs().Patch(defaults.ImageRegistryResourceName, types.MergePatchType, []byte(`{"spec": {"managementState": "Removed"}}`)); err != nil {
		if errors.IsNotFound(err) {
			// That's not exactly what we are asked for. And few seconds later
			// the operator may bootstrap it. However, if the operator is
			// disabled, it means the registry is not installed and we're
			// already in the desired state.
			return
		}
		te.Fatalf("unable to uninstall the image registry: %s", err)
	}

	var cr *imageregistryapiv1.Config
	err := wait.Poll(5*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			cr = nil
			return true, nil
		} else if err != nil {
			return false, err
		}

		conds := GetImageRegistryConditions(cr)
		te.Logf("waiting for the registry to be removed: %s", conds)
		return conds.Progressing.IsFalse() && conds.Removed.IsTrue(), nil
	})
	if err != nil {
		DumpYAML(te, "the latest observed state of the image registry resource", cr)
		DumpOperatorLogs(te)
		te.Fatalf("failed to wait for the imageregistry resource to be removed: %s", err)
	}
}

func deleteImageRegistryResource(te TestEnv) {
	// TODO(dmage): the finalizer should be removed by the operator
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cr, err := te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		} else if err != nil {
			return err
		}
		cr.Finalizers = nil
		_, err = te.Client().Configs().Update(cr)
		return err
	}); err != nil {
		te.Fatalf("unable to delete the image registry resource: %s", err)
	}

	if err := DeleteCompletely(
		func() (metav1.Object, error) {
			return te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		},
		func(deleteOptions *metav1.DeleteOptions) error {
			return te.Client().Configs().Delete(defaults.ImageRegistryResourceName, deleteOptions)
		},
	); err != nil && !errors.IsNotFound(err) {
		te.Fatalf("unable to delete the image registry resource: %s", err)
	}
}

func RemoveImageRegistry(te TestEnv) {
	te.Logf("uninstalling the image registry...")
	ensureImageRegistryToBeRemoved(te)
	te.Logf("stopping the operator...")
	if err := StopDeployment(te, te.Client(), OperatorDeploymentName, OperatorDeploymentNamespace); err != nil {
		te.Fatalf("unable to stop the operator: %s", err)
	}
	te.Logf("deleting the image registry resource...")
	deleteImageRegistryResource(te)
}

func DeployImageRegistry(te TestEnv, spec *imageregistryapiv1.ImageRegistrySpec) {
	if spec != nil {
		te.Logf("creating the image registry resource...")
		cr := &imageregistryapiv1.Config{
			ObjectMeta: metav1.ObjectMeta{
				Name: defaults.ImageRegistryResourceName,
			},
			Spec: *spec,
		}
		if _, err := te.Client().Configs().Create(cr); err != nil {
			te.Fatalf("unable to create the image registry resource: %s", err)
		}
	}

	te.Logf("starting the operator...")
	if err := startOperator(te.Client()); err != nil {
		te.Fatalf("unable to start the operator: %s", err)
	}
}

func DumpImageRegistryResource(te TestEnv) {
	cr, err := te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
	if err != nil {
		te.Logf("unable to dump the image registry resource: %s", err)
		return
	}
	DumpYAML(te, "the image registry resource", cr)
}

func DumpImageRegistryDeployment(logger Logger, client *Clientset) {
	d, err := client.Deployments(OperatorDeploymentNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		logger.Logf("unable to dump the image registry deployment: %s", err)
		return
	}
	DumpYAML(logger, "the image registry deployment", d)
}

func WaitUntilImageRegistryConfigIsProcessed(te TestEnv) *imageregistryapiv1.Config {
	var cr *imageregistryapiv1.Config
	err := wait.Poll(5*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err = te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			te.Logf("waiting for the registry: the resource does not exist")
			cr = nil
			return false, nil
		} else if err != nil {
			return false, err
		}

		conds := GetImageRegistryConditions(cr)
		te.Logf("waiting for the registry: %s", conds)
		return conds.Progressing.IsFalse() && conds.Available.IsTrue() || conds.Degraded.IsTrue(), nil
	})
	if err != nil {
		DumpYAML(te, "the latest observed state of the image registry resource", cr)
		DumpOperatorLogs(te)
		te.Fatalf("failed to wait for the imageregistry resource to be processed: %s", err)
	}
	return cr
}

func WaitUntilImageRegistryIsAvailable(te TestEnv) {
	te.Logf("waiting for the operator to deploy the registry...")

	cr := WaitUntilImageRegistryConfigIsProcessed(te)
	conds := GetImageRegistryConditions(cr)
	if conds.Progressing.IsTrue() || conds.Available.IsFalse() {
		DumpYAML(te, "the latest observed state of the image registry resource", cr)
		DumpOperatorLogs(te)
		te.Fatal("the imageregistry resource is processed, but the the image registry is not available")
	}

	te.Logf("the image registry resource reports that the registry is deployed and available")
}

func EnsureInternalRegistryHostnameIsSet(te TestEnv) {
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (bool, error) {
		cfg, err := te.Client().Images().Get("cluster", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			te.Logf("waiting for the image config resource: the resource does not exist")
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
		te.Fatalf("cluster image config resource was not updated with internal registry hostname: %v", err)
	}
}

func hasExpectedClusterOperatorConditions(status *configapiv1.ClusterOperator) bool {
	gotAvailable := false
	gotProgressing := false
	gotDegraded := false
	for _, c := range status.Status.Conditions {
		if c.Type == configapiv1.OperatorAvailable {
			gotAvailable = true
		}
		if c.Type == configapiv1.OperatorProgressing {
			gotProgressing = true
		}
		if c.Type == configapiv1.OperatorDegraded {
			gotDegraded = true
		}
	}
	return gotAvailable && gotProgressing && gotDegraded
}

func EnsureClusterOperatorStatusIsSet(te TestEnv) *configapiv1.ClusterOperator {
	var status *configapiv1.ClusterOperator
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		status, err = te.Client().ClusterOperators().Get(defaults.ImageRegistryClusterOperatorResourceName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			te.Logf("waiting for the cluster operator resource: the resource does not exist")
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
		if status != nil {
			te.Logf("clusteroperator conditions are: %#v", status.Status.Conditions)
		}
		te.Fatalf("clusteroperator status resource was not updated with the expected status: %v", err)
	}
	return status
}

func EnsureClusterOperatorStatusIsNormal(te TestEnv) {
	clusterOperator := EnsureClusterOperatorStatusIsSet(te)

	for _, cond := range clusterOperator.Status.Conditions {
		switch cond.Type {
		case configapiv1.OperatorAvailable:
			if cond.Status != configapiv1.ConditionTrue {
				te.Errorf("Expected clusteroperator Available=%s, got %s", configapiv1.ConditionTrue, cond.Status)
			}
		case configapiv1.OperatorProgressing:
			if cond.Status != configapiv1.ConditionFalse {
				te.Errorf("Expected clusteroperator Progressing=%s, got %s", configapiv1.ConditionFalse, cond.Status)
			}
		case configapiv1.OperatorDegraded:
			if cond.Status != configapiv1.ConditionFalse {
				te.Errorf("Expected clusteroperator Degraded=%s, got %s", configapiv1.ConditionFalse, cond.Status)
			}
		}
	}

	namespaceFound := false
	for _, obj := range clusterOperator.Status.RelatedObjects {
		if strings.ToLower(obj.Resource) == "namespaces" {
			namespaceFound = true
			if obj.Name != defaults.ImageRegistryOperatorNamespace {
				te.Errorf("expected related namespaces resource to have name %q, got %q", defaults.ImageRegistryOperatorNamespace, obj.Name)
			}
		}
	}
	if !namespaceFound {
		te.Error("could not find related object namespaces")
	}
}

func EnsureOperatorIsNotHotLooping(te TestEnv) {
	// Allow the operator a few seconds to stabilize
	time.Sleep(15 * time.Second)
	var cfg *imageregistryapiv1.Config
	var err error
	err = wait.Poll(1*time.Second, 30*time.Second, func() (stop bool, err error) {
		cfg, err = te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if err != nil || cfg == nil {
			te.Logf("failed to retrieve registry operator config: %v", err)
			return false, nil
		}
		return true, nil
	})
	if cfg == nil || err != nil {
		te.Errorf("failed to retrieve registry operator config: %v", err)
	}
	oldVersion := cfg.ResourceVersion

	// wait 15s and then ensure that ResourceVersion is not updated. If it was updated then something
	// is updating the registry config resource when we should be at steady state.
	time.Sleep(15 * time.Second)
	err = wait.Poll(1*time.Second, 30*time.Second, func() (stop bool, err error) {
		cfg, err = te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
		if err != nil || cfg == nil {
			te.Logf("failed to retrieve registry operator config: %v", err)
			return false, nil
		}
		return true, nil
	})
	if cfg == nil || err != nil {
		te.Errorf("failed to retrieve registry operator config: %v", err)
	}
	if oldVersion != cfg.ResourceVersion {
		te.Errorf("registry config resource version was updated when it should have been stable, went from %s to %s", oldVersion, cfg.ResourceVersion)
	}
}

func PlatformHasDefaultStorage(te TestEnv) bool {
	return !PlatformIsOneOf(te, []configapiv1.PlatformType{
		configapiv1.BareMetalPlatformType,
		configapiv1.VSpherePlatformType,
	})
}

func SetupAvailableImageRegistry(t *testing.T, spec *imageregistryapiv1.ImageRegistrySpec) TestEnv {
	te := Setup(t)

	noStorage := (spec == nil || spec.Storage == imageregistryapiv1.ImageRegistryConfigStorage{})
	if noStorage && !PlatformHasDefaultStorage(te) {
		t.Skip("skipping because the current platform does not provide default storage configuration")
	}

	defer func() {
		if te.Failed() {
			TeardownImageRegistry(te)
		}
	}()

	DeployImageRegistry(te, spec)
	WaitUntilImageRegistryIsAvailable(te)
	EnsureClusterOperatorStatusIsNormal(te)
	return te
}

func TeardownImageRegistry(te TestEnv) {
	defer func() {
		RemoveImageRegistry(te)
	}()
	if te.Failed() {
		DumpImageRegistryResource(te)
		DumpOperatorDeployment(te)
		DumpOperatorLogs(te)
	}
}
