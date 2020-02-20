package e2e

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestDegraded(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	framework.MustDeployImageRegistry(t, client, &imageregistryv1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaults.ImageRegistryResourceName,
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			ManagementState: operatorapi.Managed,
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas: -1,
		},
	})
	cr := framework.MustEnsureImageRegistryIsProcessed(t, client)

	var degraded operatorapi.OperatorCondition
	for _, cond := range cr.Status.Conditions {
		switch cond.Type {
		case operatorapi.OperatorStatusTypeDegraded:
			degraded = cond
		}
	}
	if degraded.Status != operatorapi.ConditionTrue {
		framework.DumpObject(t, "the latest observed image registry resource", cr)
		framework.DumpOperatorLogs(t, client)
		t.Fatal("the imageregistry resource is expected to be degraded")
	}

	if expected := "replicas must be greater than or equal to 0"; !strings.Contains(degraded.Message, expected) {
		t.Errorf("expected degraded message to contain %q, got %q", expected, degraded.Message)
	}
}
