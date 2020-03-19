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
	client := framework.MustNewClientset(t, nil)

	// TODO: Move these checks to a conformance test run on all providers
	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
	framework.MustEnsureOperatorIsNotHotLooping(t, client)
	framework.MustEnsureServiceCAConfigMap(t, client)
	framework.MustEnsureNodeCADaemonSetIsAvailable(t, client)

	cr, err := client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image registry custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}
	// Check that the cronjob was created
	cronjob, err := client.BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get("image-pruner", metav1.GetOptions{})
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
	if _, err := client.Configs().Update(cr); err != nil {
		t.Errorf("unable to update image registry custom resource: %s", err)
	}

	var errs []error

	// Wait for the cronjob to have an updated --prune-registry flag
	err = wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		errs = nil
		// Get an updated version of the cronjob
		cronjob, err = client.BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get("image-pruner", metav1.GetOptions{})
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
	client := framework.MustNewClientset(t, nil)

	// TODO: Move these checks to a conformance test run on all providers
	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
	framework.MustEnsureOperatorIsNotHotLooping(t, client)
	framework.MustEnsureServiceCAConfigMap(t, client)
	framework.MustEnsureNodeCADaemonSetIsAvailable(t, client)

	// Check that the pruner custom resource was created
	cr, err := client.ImagePruners().Get(defaults.ImageRegistryImagePrunerResourceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image pruner custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	// Check that the cronjob was created
	_, err = client.BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get("image-pruner", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image pruner cronjob: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	// Check that the Available condition is set for the pruner
	errs := framework.PrunerConditionExistsWithStatusAndReason(client, "Available", operatorapi.ConditionTrue, "Ready")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
			framework.DumpImagePrunerResource(t, client)
		}
	}

	// Check that the Scheduled condition is set for the cronjob
	errs = framework.PrunerConditionExistsWithStatusAndReason(client, "Scheduled", operatorapi.ConditionTrue, "Scheduled")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
			framework.DumpImagePrunerResource(t, client)
		}
	}

	// Check that the Failed condition is set correctly for the last job run
	errs = framework.PrunerConditionExistsWithStatusAndReason(client, "Failed", operatorapi.ConditionFalse, "Complete")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
			framework.DumpImagePrunerResource(t, client)
		}
	}

	// Check that making changes to the pruner custom resource trickle down to the cronjob
	// and that the conditions get updated correctly
	truePtr := true
	cr.Spec.Suspend = &truePtr
	cr.Spec.Schedule = "10 10 * * *"
	_, err = client.ImagePruners().Update(cr)
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image registry pruner custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	// Check that the Scheduled condition is set for the cronjob
	errs = framework.PrunerConditionExistsWithStatusAndReason(client, "Scheduled", operatorapi.ConditionFalse, "Suspended")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
			framework.DumpImagePrunerResource(t, client)
		}
	}

	cronjob, err := client.BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get("image-pruner", metav1.GetOptions{})
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
	cr, err = client.ImagePruners().Get(defaults.ImageRegistryImagePrunerResourceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image registry pruner custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

	falsePtr := false
	cr.Spec.Suspend = &falsePtr
	cr.Spec.Schedule = ""
	_, err = client.ImagePruners().Update(cr)
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image registry pruner custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

}
