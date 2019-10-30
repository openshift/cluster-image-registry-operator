package e2e

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configapiv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestBaremetalDefaults(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	infrastructureConfig, err := client.Infrastructures().Get("cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if infrastructureConfig.Status.PlatformStatus.Type != configapiv1.BareMetalPlatformType {
		t.Skip("skipping on non-BareMetal platform")
	}

	// Start of the meaningful part
	defer framework.MustRemoveImageRegistry(t, client)

	framework.MustDeployImageRegistry(t, client, nil)
	cr := framework.MustEnsureImageRegistryIsProcessed(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

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
