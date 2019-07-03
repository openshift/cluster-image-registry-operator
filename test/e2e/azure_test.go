package e2e

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestAzureDefaults(t *testing.T) {
	kcfg, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("error building kubeconfig: %s", err)
	}

	installConfig, err := clusterconfig.GetInstallConfig(kcfg)
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}
	if installConfig.Platform.Azure == nil {
		t.Skip("skipping on non-Azure platform")
	}

	client := framework.MustNewClientset(t, kcfg)
	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
	framework.MustEnsureOperatorIsNotHotLooping(t, client)

	// Check that the image registry config resource exists and contains correct storage status.
	cr, err := client.Configs().Get(imageregistryv1.ImageRegistryResourceName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unable to get image registry config: %s", err)
	}
	if cr.Spec.Storage.Azure == nil {
		framework.DumpImageRegistryResource(t, client)
		framework.DumpOperatorLogs(t, client)
		t.Fatalf("the image registry config is missing the Azure configuration")
	}

	if cr.Spec.Storage.Azure.AccountName == "" {
		t.Errorf("the image registry config contains incorrect data: accountName is empty")
	}
	if cr.Spec.Storage.Azure.Container == "" {
		t.Errorf("the image registry config contains incorrect data: container is empty")
	}
	if !cr.Status.StorageManaged {
		t.Errorf("the image registry config contains incorrect data: storageManaged is false")
	}

	// Wait for the image registry resource to have an updated StorageExists condition
	errs := framework.ConditionExistsWithStatusAndReason(client, imageregistryv1.StorageExists, operatorapi.ConditionTrue, "ContainerExists")
	for _, err := range errs {
		t.Error(err)
	}
}
