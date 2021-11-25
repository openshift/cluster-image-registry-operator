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
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

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

	if buildName, err := runTestBuild(ctx, te, nsName); err != nil {
		te.Error(err)
		dumpBuildInfo(ctx, te, nsName, buildName)
	}

	// This ensures that we can remove the image registry
	framework.RemoveImageRegistry(te)
}

// runTestBuild runs a build which pushes an image to an imagestream.
// Returns the name of the build, and an error if any.
func runTestBuild(ctx context.Context, te framework.TestEnv, nsName string) (string, error) {
	te.Logf("creating build output imagestream")
	outputIs := &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodejs-out",
			Namespace: nsName,
		},
	}
	_, err := te.Client().ImageInterface.ImageStreams(nsName).Create(ctx, outputIs, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create build output imagestream: %v", err)
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
		return "", fmt.Errorf("failed to create build %s/%s: %v", nsName, build.Name, err)
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
		return build.Name, fmt.Errorf("error waiting for build to complete: %v", err)
	}

	finishedBuild, err := te.Client().BuildInterface.Builds(nsName).Get(ctx, build.Name, metav1.GetOptions{})
	if err != nil {
		return build.Name, fmt.Errorf("failed to get finished build %s/%s: %v", nsName, build.Name, err)
	}

	if finishedBuild.Status.Phase == buildv1.BuildPhaseError || finishedBuild.Status.Phase == buildv1.BuildPhaseFailed {
		return build.Name, fmt.Errorf("build %s/%s failed with message: %s", finishedBuild.Namespace, finishedBuild.Name, finishedBuild.Status.Message)
	}

	return build.Name, nil
}

// dumpBuildInfo dumps build related information to the log. Includes the following:
//
// 1. Build logs
// 2. Build pod YAML
// 3. ConfigMaps in the build's namespace
func dumpBuildInfo(ctx context.Context, te framework.TestEnv, nsName string, buildName string) {
	if buildName == "" {
		return
	}
	te.Log("attempting to dump build information")
	buildPodName := fmt.Sprintf("%s-build", buildName)
	buildLogs, err := framework.GetLogsForPod(te.Client(), nsName, buildPodName)
	if err != nil {
		te.Logf("failed to get build logs: %v", err)
	} else {
		te.Logf("Build logs:")
		framework.DumpPodLogs(te, buildLogs)
	}

	build, err := te.Client().BuildInterface.Builds(nsName).Get(ctx, buildName, metav1.GetOptions{})
	if err != nil {
		te.Logf("failed to get build YAML: %v", err)
	} else {
		framework.DumpYAML(te, "build YAML", build)
	}

	buildPod, err := te.Client().CoreV1Interface.Pods(nsName).Get(ctx, buildPodName, metav1.GetOptions{})
	if err != nil {
		te.Logf("failed to get build pod YAML: %v", err)
	} else {
		framework.DumpYAML(te, "build pod YAML", buildPod)
	}

	configMaps, err := te.Client().CoreV1Interface.ConfigMaps(nsName).List(ctx, metav1.ListOptions{})
	if err != nil {
		te.Logf("failed to get ConfigMaps: %v", err)
	} else {
		framework.DumpYAML(te, "build ConfigMaps", configMaps)
	}
}
