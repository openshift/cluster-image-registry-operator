package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	buildv1 "github.com/openshift/api/build/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	imagev1 "github.com/openshift/api/image/v1"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

// TestImageRegistryRemovedWithImages verifies that we can tear down the image registry if images had been
// imported or pushed to it.
// Some cloud providers do not allow storage buckets to be removed unless the bucket is empty.
func TestImageRegistryRemovedWithImages(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	// Deploy the image registry using platform defaults
	framework.DeployImageRegistry(te, nil)
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	ctx := context.Background()

	nsName := "e2e-image-registry-removed"
	te.Logf("creating test namespace %s", nsName)
	ns, err := te.Client().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}, metav1.CreateOptions{})

	if err != nil {
		te.Fatalf("failed to create test namespace: %v", err)
	}

	defer func() {
		err = te.Client().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
		if err != nil {
			te.Errorf("failed to delete namespace %s: %v", ns.Name, err)
		}
	}()

	te.Logf("creating build output imagestream")
	outputIs := &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodejs-out",
			Namespace: nsName,
		},
	}
	_, err = te.Client().ImageInterface.ImageStreams(nsName).Create(ctx, outputIs, metav1.CreateOptions{})
	if err != nil {
		te.Errorf("failed to create build output imagestream: %v", err)
	}

	te.Logf("starting build")
	build := &buildv1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodejs-ex-1",
			Namespace: nsName,
		},
		Spec: buildv1.BuildSpec{
			CommonSpec: buildv1.CommonSpec{
				Source: buildv1.BuildSource{
					Git: &buildv1.GitBuildSource{
						URI: "https://github.com/sclorg/nodejs-ex.git",
					},
				},
				Strategy: buildv1.BuildStrategy{
					SourceStrategy: &buildv1.SourceBuildStrategy{
						From: corev1.ObjectReference{
							Kind:      "ImageStreamTag",
							Name:      "nodejs:latest",
							Namespace: "openshift",
						},
					},
				},
				Output: buildv1.BuildOutput{
					To: &corev1.ObjectReference{
						Kind: "ImageStreamTag",
						Name: "nodejs-out:latest",
					},
				},
			},
		},
	}
	_, err = te.Client().BuildInterface.Builds(nsName).Create(ctx, build, metav1.CreateOptions{})
	if err != nil {
		te.Errorf("failed to create build %s/%s: %v", nsName, build.Name, err)
	}

	te.Logf("waiting for build %s/%s to complete", build.Namespace, build.Name)
	err = wait.Poll(5*time.Second, 10*time.Minute, func() (bool, error) {
		runningBuild, err := te.Client().BuildInterface.Builds(nsName).Get(ctx, build.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range runningBuild.Status.Conditions {
			if string(cond.Type) == string(buildv1.BuildPhaseComplete) && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		te.Errorf("error waiting for build to complete: %v", err)
		buildLogs, err := framework.GetLogsForPod(te.Client(), build.Namespace, fmt.Sprintf("%s-build", build.Name))
		if err != nil {
			te.Logf("failed to get build logs: %v", err)
		} else {
			framework.DumpPodLogs(te, buildLogs)
		}
	}
	finishedBuild, err := te.Client().BuildInterface.Builds(nsName).Get(ctx, build.Name, metav1.GetOptions{})
	if finishedBuild.Status.Phase == buildv1.BuildPhaseError || finishedBuild.Status.Phase == buildv1.BuildPhaseFailed {
		te.Errorf("build %s/%s failed with message: %s", finishedBuild.Namespace, finishedBuild.Name, finishedBuild.Status.Message)
	}

	// This ensures that we can remove the image registry
	framework.RemoveImageRegistry(te)
}
