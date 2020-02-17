package e2e

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configapiv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestBaremetalAndVSphereDefaults(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	infrastructureConfig, err := te.Client().Infrastructures().Get("cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if infrastructureConfig.Status.PlatformStatus.Type != configapiv1.BareMetalPlatformType &&
		infrastructureConfig.Status.PlatformStatus.Type != configapiv1.VSpherePlatformType {
		t.Skip("skipping on non-BareMetal non-VSphere platform")
	}

	framework.DeployImageRegistry(te, nil)
	cr := framework.WaitUntilImageRegistryConfigIsProcessed(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)

	conds := framework.GetImageRegistryConditions(cr)
	if conds.Available.Reason() != "Removed" {
		t.Errorf("exp Available reason: Removed, got %s", conds.Available.Reason())
	}
	if conds.Degraded.Reason() != "Removed" {
		t.Errorf("exp Degraded reason: Removed, got %s", conds.Degraded.Reason())
	}
	if conds.Progressing.Reason() != "Removed" {
		t.Errorf("exp Progressing reason: Removed, got %s", conds.Progressing.Reason())
	}
}
