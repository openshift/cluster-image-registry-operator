package e2e

import (
	"fmt"
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

	_, err = client.Infrastructures().Update(&configapiv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name:            infrastructureConfig.Name,
			ResourceVersion: infrastructureConfig.ResourceVersion,
		},
		Status: configapiv1.InfrastructureStatus{
			InfrastructureName: infrastructureConfig.Status.InfrastructureName,
			PlatformStatus: &configapiv1.PlatformStatus{
				Type: configapiv1.BareMetalPlatformType,
			},
			EtcdDiscoveryDomain:  infrastructureConfig.Status.EtcdDiscoveryDomain,
			APIServerURL:         infrastructureConfig.Status.APIServerURL,
			APIServerInternalURL: infrastructureConfig.Status.APIServerInternalURL,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Reset infrastructures.config.openshift.io to it's original state at the end of the test
	defer func() {
		previousConfig, err := client.Infrastructures().Get("cluster", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		infrastructureConfig.ResourceVersion = previousConfig.ResourceVersion
		_, err = client.Infrastructures().Update(infrastructureConfig)
		if err != nil {
			panic(fmt.Errorf("unable to restore infrastructure cluster config: %s; %v", err, infrastructureConfig))
		}
	}()

	// Start of the meaningful part
	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	cr := framework.MustEnsureImageRegistryIsProcessed(t, client)
	conds := framework.GetImageRegistryConditions(cr)
	if !conds.Degraded.IsTrue() {
		t.Errorf("the operator is expected to be degraded, got: %s", conds)
	}
	if want := "StorageNotConfigured"; conds.Degraded.Reason() != want {
		t.Errorf("degraded reason: got %q, want %q", conds.Degraded.Reason(), want)
	}

	clusterOperator := framework.MustEnsureClusterOperatorStatusIsSet(t, client)
	for _, cond := range clusterOperator.Status.Conditions {
		switch cond.Type {
		case configapiv1.OperatorAvailable:
			if cond.Status != configapiv1.ConditionFalse {
				t.Errorf("expected clusteroperator to report Available=%s, got %s", configapiv1.ConditionFalse, cond.Status)
			}
		case configapiv1.OperatorDegraded:
			if cond.Status != configapiv1.ConditionTrue {
				t.Errorf("expected clusteroperator to report Degraded=%s, got %s", configapiv1.ConditionTrue, cond.Status)
			}
			if cond.Reason != "StorageNotConfigured" {
				t.Errorf("expected clusteroprator degraded status reason to be %s, got %s", "StorageNotConfigured", cond.Reason)
			}
		}
	}

}
