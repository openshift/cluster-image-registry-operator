package e2e

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

// TestPruneRegistry ensures that the value for the --prune-registry flag
// is set correctly based on the image registry's custom resources
// Spec.ManagementState field
func TestPruneRegistryFlag(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	// TODO: Move these checks to a conformance test run on all providers
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureOperatorIsNotHotLooping(te)
	framework.EnsureServiceCAConfigMap(te)
	framework.EnsureNodeCADaemonSetIsAvailable(te)

	cr, err := te.Client().Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image registry custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}
	// Check that the cronjob was created
	cronjob, err := te.Client().BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get("image-pruner", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image pruner cronjob: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	// Check that the image registry is in the Managed state
	if cr.Spec.ManagementState != operatorapi.Managed {
		t.Errorf("the image registry Spec.ManagementState should be Managed but was %s instead: %s", cr.Spec.ManagementState, err)
	}

	// Check that the --prune-registry flag is true on the pruning cronjob
	if err := framework.FlagExistsWithValue(cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args, "--prune-registry", "true"); err != nil {
		t.Errorf("%v", err)
	}

	cr.Spec.ManagementState = operatorapi.Removed

	// Set the image registry to be Removed
	if _, err := te.Client().Configs().Update(cr); err != nil {
		t.Errorf("unable to update image registry custom resource: %s", err)
	}

	var errs []error

	// Wait for the cronjob to have an updated --prune-registry flag
	err = wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		errs = nil
		// Get an updated version of the cronjob
		cronjob, err = te.Client().BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get("image-pruner", metav1.GetOptions{})
		if err != nil {
			return true, err
		}

		// Check if the --prune-registry flag is now false on the pruning cronjob
		if err = framework.FlagExistsWithValue(cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args, "--prune-registry", "false"); err != nil {
			errs = append(errs, err)
			return false, nil
		}

		return true, nil

	})
	if err != nil {
		errs = append(errs, err)
	}
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

}

// TestPruner verifies that the pruner controller installs the cronjob and sets it's
// conditions appropriately
func TestPruner(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	defer func() {
		if t.Failed() {
			framework.DumpImagePrunerResource(t, te.Client())
		}
	}()

	// TODO: Move these checks to a conformance test run on all providers
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureOperatorIsNotHotLooping(te)
	framework.EnsureServiceCAConfigMap(te)
	framework.EnsureNodeCADaemonSetIsAvailable(te)

	// Check that the pruner custom resource was created
	cr, err := te.Client().ImagePruners().Get(defaults.ImageRegistryImagePrunerResourceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image pruner custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	// Check that the cronjob was created
	_, err = te.Client().BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get("image-pruner", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image pruner cronjob: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	// Check that the Available condition is set for the pruner
	framework.PrunerConditionExistsWithStatusAndReason(te, "Available", operatorapi.ConditionTrue, "Ready")

	// Check that the Scheduled condition is set for the cronjob
	framework.PrunerConditionExistsWithStatusAndReason(te, "Scheduled", operatorapi.ConditionTrue, "Scheduled")

	// Check that the Failed condition is set correctly for the last job run
	framework.PrunerConditionExistsWithStatusAndReason(te, "Failed", operatorapi.ConditionFalse, "Complete")

	// Check that making changes to the pruner custom resource trickle down to the cronjob
	// and that the conditions get updated correctly
	truePtr := true
	cr.Spec.Suspend = &truePtr
	cr.Spec.Schedule = "10 10 * * *"
	_, err = te.Client().ImagePruners().Update(cr)
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image registry pruner custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	// Check that the Scheduled condition is set for the cronjob
	framework.PrunerConditionExistsWithStatusAndReason(te, "Scheduled", operatorapi.ConditionFalse, "Suspended")

	cronjob, err := te.Client().BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get("image-pruner", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image pruner cronjob: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	if *cronjob.Spec.Suspend != true {
		t.Errorf("The cronjob Spec.Suspend field should have been true, but was %v instead", *cronjob.Spec.Suspend)
	}

	if cronjob.Spec.Schedule != "10 10 * * *" {
		t.Errorf("The cronjob Spec.Schedule field should have been '10 10 * * *' but was %v instead", cronjob.Spec.Schedule)
	}

	// Reset the CR
	cr, err = te.Client().ImagePruners().Get(defaults.ImageRegistryImagePrunerResourceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image registry pruner custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	falsePtr := false
	cr.Spec.Suspend = &falsePtr
	cr.Spec.Schedule = ""
	_, err = te.Client().ImagePruners().Update(cr)
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image registry pruner custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}
}
