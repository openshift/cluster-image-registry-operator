package e2e

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapi "github.com/openshift/api/operator/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

// TestPrunerInstalled is a test to verify that the pruner cronjob
// is setup and running
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
		t.Errorf("unable to get the image registry pruner custom resource: %s", err)
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
	cr, err = client.ImagePruners().Update(cr)
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
	cr, err = client.ImagePruners().Update(cr)
	if errors.IsNotFound(err) {
		t.Errorf("unable to get the image registry pruner custom resource: %s", err)
	} else if err != nil {
		t.Fatal(err)
	}

}
