package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

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

	cr, err := te.Client().Configs().Get(
		context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Check that the cronjob was created
	cronjob, err := te.Client().BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), "image-pruner", metav1.GetOptions{},
	)
	if err != nil {
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
	if _, err := te.Client().Configs().Update(
		context.Background(), cr, metav1.UpdateOptions{},
	); err != nil {
		t.Fatalf("unable to update image registry custom resource: %s", err)
	}

	var errs []error

	// Wait for the cronjob to have an updated --prune-registry flag
	err = wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		errs = nil
		// Get an updated version of the cronjob
		cronjob, err = te.Client().BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get(
			context.Background(), "image-pruner", metav1.GetOptions{},
		)
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
	cr, err := te.Client().ImagePruners().Get(
		context.Background(), defaults.ImageRegistryImagePrunerResourceName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !cr.Spec.IgnoreInvalidImageReferences {
		t.Errorf("the default pruner config should have spec.ignoreInvalidImageReferences set to true, but it doesn't")
	}

	// Check that the cronjob was created
	cronjob, err := te.Client().BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), "image-pruner", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the Available condition is set for the pruner
	framework.PrunerConditionExistsWithStatusAndReason(te, "Available", operatorapi.ConditionTrue, "AsExpected")

	// Check that the Scheduled condition is set for the cronjob
	framework.PrunerConditionExistsWithStatusAndReason(te, "Scheduled", operatorapi.ConditionTrue, "Scheduled")

	// Check that the Failed condition is set correctly for the last job run
	framework.PrunerConditionExistsWithStatusAndReason(te, "Failed", operatorapi.ConditionFalse, "Complete")

	if !containsString(cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args, "--ignore-invalid-refs=true") {
		defer framework.DumpYAML(t, "cronjob", cronjob)
		t.Fatalf("flag --ignore-invalid-refs=true is not found")
	}

	if !containsString(cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args, "--registry-url=https://image-registry.openshift-image-registry.svc:5000") {
		defer framework.DumpYAML(t, "cronjob", cronjob)
		t.Fatalf("flag --registry-url is not found")
	}

	// Check that making changes to the pruner custom resource trickle down to the cronjob
	// and that the conditions get updated correctly
	truePtr := true
	cr.Spec.Suspend = &truePtr
	cr.Spec.Schedule = "10 10 * * *"
	cr.Spec.IgnoreInvalidImageReferences = false
	_, err = te.Client().ImagePruners().Update(
		context.Background(), cr, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Reset the CR
		cr, err := te.Client().ImagePruners().Get(
			context.Background(), defaults.ImageRegistryImagePrunerResourceName, metav1.GetOptions{},
		)
		if err != nil {
			t.Fatal(err)
		}

		falsePtr := false
		cr.Spec.Suspend = &falsePtr
		cr.Spec.Schedule = ""
		_, err = te.Client().ImagePruners().Update(
			context.Background(), cr, metav1.UpdateOptions{},
		)
		if err != nil {
			t.Fatal(err)
		}
	}()

	// Check that the Scheduled condition is set for the cronjob
	framework.PrunerConditionExistsWithStatusAndReason(te, "Scheduled", operatorapi.ConditionFalse, "Suspended")

	cronjob, err = te.Client().BatchV1beta1Interface.CronJobs(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), "image-pruner", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if *cronjob.Spec.Suspend != true {
		t.Errorf("The cronjob Spec.Suspend field should have been true, but was %v instead", *cronjob.Spec.Suspend)
	}

	if cronjob.Spec.Schedule != "10 10 * * *" {
		t.Errorf("The cronjob Spec.Schedule field should have been '10 10 * * *' but was %v instead", cronjob.Spec.Schedule)
	}

	if !containsString(cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args, "--ignore-invalid-refs=false") {
		t.Fatalf("The cronjob container arguments should contain --ignore-invalid-refs=false, but arguments are %v", cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args)
	}
}

func TestPrunerPodCompletes(t *testing.T) {
	ctx := context.Background()
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	cr, err := te.Client().ImagePruners().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	origSpec := cr.Spec.DeepCopy()

	suspend := false
	cr.Spec.Suspend = &suspend
	cr.Spec.Schedule = "* * * * *"
	_, err = te.Client().ImagePruners().Update(
		context.Background(), cr, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cr, err := te.Client().ImagePruners().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}

		cr.Spec = *origSpec

		_, err = te.Client().ImagePruners().Update(ctx, cr, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}()

	t.Logf("waiting the pruner to succeed...")
	err = wait.Poll(5*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		pods, err := te.Client().Pods(defaults.ImageRegistryOperatorNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}

		for _, pod := range pods.Items {
			if !strings.HasPrefix(pod.Name, "image-pruner-") {
				continue
			}
			t.Logf("%s: %s", pod.Name, pod.Status.Phase)
			if pod.Status.Phase == "Succeeded" {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrunerIgnoreInvalidImageReferences(t *testing.T) {
	ctx := context.Background()
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	cr, err := te.Client().ImagePruners().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	origSpec := cr.Spec.DeepCopy()

	suspend := false
	cr.Spec.Suspend = &suspend
	cr.Spec.Schedule = "* * * * *"
	cr.Spec.IgnoreInvalidImageReferences = true
	_, err = te.Client().ImagePruners().Update(
		context.Background(), cr, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cr, err := te.Client().ImagePruners().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}

		cr.Spec = *origSpec

		_, err = te.Client().ImagePruners().Update(ctx, cr, metav1.UpdateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}()

	t.Logf("waiting the pruner config to be observed...")
	err = wait.Poll(5*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err := te.Client().ImagePruners().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		return cr.Status.ObservedGeneration == cr.Generation, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	cronjob, err := te.Client().BatchV1beta1Interface.CronJobs("openshift-image-registry").Get(ctx, "image-pruner", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !containsString(cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args, "--ignore-invalid-refs=true") {
		defer framework.DumpYAML(t, "cronjob", cronjob)
		t.Fatalf("flag --ignore-invalid-refs=true is not found")
	}

	t.Logf("waiting the pruner to succeed...")
	err = wait.Poll(5*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		pods, err := te.Client().Pods(defaults.ImageRegistryOperatorNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}

		for _, pod := range pods.Items {
			if !strings.HasPrefix(pod.Name, "image-pruner-") {
				continue
			}
			if !containsString(pod.Spec.Containers[0].Args, "--ignore-invalid-refs=true") {
				// pod from another test?
				t.Logf("pod %s has wrong arguments", pod.Name)
				continue
			}
			t.Logf("%s: %s", pod.Name, pod.Status.Phase)
			if pod.Status.Phase == "Succeeded" {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
