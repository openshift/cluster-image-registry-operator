package e2e_test

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestLeaderElection(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorapi.Managed,
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
	})
	defer framework.TeardownImageRegistry(te)

	if _, err := framework.WaitForRegistryDeployment(te.Client()); err != nil {
		t.Fatalf("error awaiting for registry deployment: %v", err)
	}

	var numberOfReplicas = int32(3)
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		deploy, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
			context.Background(), "cluster-image-registry-operator", metav1.GetOptions{},
		)
		if err != nil {
			return err
		}

		deploy.Spec.Replicas = &numberOfReplicas

		_, err = te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Update(
			context.Background(), deploy, metav1.UpdateOptions{},
		)
		return err
	}); err != nil {
		t.Fatalf("error updating number of operator replicas: %v", err)
	}

	framework.WaitUntilDeploymentIsRolledOut(
		te, defaults.ImageRegistryOperatorNamespace, "cluster-image-registry-operator",
	)

	allLogs, err := framework.GetOperatorLogs(te.Client())
	if err != nil {
		t.Fatalf("error reading operator logs: %v", err)
	}

	awaitingPods := int32(0)
	for _, podLogs := range allLogs {
		for containerName, containerLogs := range podLogs {
			if strings.Contains(containerName, "watch") {
				continue
			}

			lastLine := len(containerLogs) - 1
			if lastLine < 0 {
				continue
			}

			if strings.Contains(containerLogs[lastLine], "attempting to acquire leader lease") {
				awaitingPods++
			}
		}
	}

	numberOfAwaitingPods := numberOfReplicas - 1
	if awaitingPods != numberOfAwaitingPods {
		t.Errorf("multiple operators running at the same time")
	}
}
