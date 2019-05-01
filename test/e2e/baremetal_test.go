package e2e

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

const (
	installerConfigNamespace = "kube-system"
	installerConfigName      = "cluster-config-v1"
)

func TestBaremetalDefaults(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	// We don't have CI for baremetal, so let's fake this environment.
	clusterConfig, err := client.ConfigMaps(installerConfigNamespace).Get(installerConfigName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	err = client.ConfigMaps(installerConfigNamespace).Delete(installerConfigName, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = client.ConfigMaps(installerConfigNamespace).Delete(installerConfigName, nil)
		if err != nil && !errors.IsNotFound(err) {
			panic(fmt.Errorf("unable to delete fake cluster config: %s", err))
		}
		clusterConfig.ResourceVersion = "" // should not be set on objects to be created
		_, err = client.ConfigMaps(installerConfigNamespace).Create(clusterConfig)
		if err != nil {
			panic(fmt.Errorf("unable to restore cluster config: %s; %s=%v", err, installerConfigName, clusterConfig))
		}
	}()
	_, err = client.ConfigMaps(installerConfigNamespace).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: installerConfigName,
		},
		Data: map[string]string{
			"install-config": `apiVersion: v1beta4`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

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
		case configv1.OperatorAvailable:
			if cond.Status != configv1.ConditionFalse {
				t.Errorf("expected clusteroperator to report Available=%s, got %s", configv1.ConditionFalse, cond.Status)
			}
		case configv1.OperatorDegraded:
			if cond.Status != configv1.ConditionTrue {
				t.Errorf("expected clusteroperator to report Degraded=%s, got %s", configv1.ConditionTrue, cond.Status)
			}
			if cond.Reason != "StorageNotConfigured" {
				t.Errorf("expected clusteroprator degraded status reason to be %s, got %s", "StorageNotConfigured", cond.Reason)
			}
		}
	}

}
