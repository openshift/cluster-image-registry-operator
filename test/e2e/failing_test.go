package e2e

import (
	"strings"
	"testing"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestDegraded(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	framework.DeployImageRegistry(te, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorapi.Managed,
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: -1,
	})
	cr := framework.EnsureImageRegistryIsProcessed(te)

	var degraded operatorapi.OperatorCondition
	for _, cond := range cr.Status.Conditions {
		switch cond.Type {
		case operatorapi.OperatorStatusTypeDegraded:
			degraded = cond
		}
	}
	if degraded.Status != operatorapi.ConditionTrue {
		framework.DumpObject(t, "the latest observed image registry resource", cr)
		framework.DumpOperatorLogs(te)
		t.Fatal("the imageregistry resource is expected to be degraded")
	}

	if expected := "replicas must be greater than or equal to 0"; !strings.Contains(degraded.Message, expected) {
		t.Errorf("expected degraded message to contain %q, got %q", expected, degraded.Message)
	}
}
