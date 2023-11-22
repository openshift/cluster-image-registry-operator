package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	autorestazure "github.com/Azure/go-autorest/autorest/azure"
	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/azure"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/azure/azureclient"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
	"github.com/openshift/cluster-image-registry-operator/test/framework/mock/listers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// TestAzurePrivateStorageAccount ensures that the operator configures the storage
// as "internal". In simplistic terms, this means to: 1. configure a private endpoint
// for the storage account; and 2. turn off public network access for the storage
// account.
//
// This test ensures the functionality works by:
//  1. set vnet and subnet name in the operator config (which signalises to the
//     operator that it should configure the storage acccount to be "internal")
//  2. verify that the private endpoint was created in Azure and the storage
//     account network is *not* publicly accessible
//  3. set management state to "Removed"
//  4. verify that the private endpoint was removed in Azure
func TestAzurePrivateStorageAccount(t *testing.T) {
	ctx := context.Background()

	kcfg, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("Error building kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kcfg)
	if err != nil {
		t.Fatalf("unable to create mock lister: %v", err)
	}

	mockLister, err := newMockLister.GetListers()
	if err != nil {
		t.Fatalf("unable to get listers from mock lister: %v", err)
	}

	infra, err := util.GetInfrastructure(mockLister.StorageListers.Infrastructures)
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.AzurePlatformType {
		t.Skip("skipping on non-Azure platform")
	}

	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	framework.DeployImageRegistry(te, nil)
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsSet(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	// the operator doesn't yet know how to discover this.
	// in the future, it will likely get them from Azure API directly.
	// for this test, this is good enough.
	msList, err := te.Client().MachineSetInterface.List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("could not list machine sets for getting networking details: %q", err)
	}

	networking := struct {
		VNet   string
		Subnet string
	}{}
	if err := json.Unmarshal(msList.Items[0].Spec.Template.Spec.ProviderSpec.Value.Raw, &networking); err != nil {
		t.Fatalf("failed to unmarshal networking values from machineset: %q", err)
	}

	vnetName := networking.VNet
	subnetName := networking.Subnet

	// patch the config - this will cause the operator to reconcile and
	// configure the private endpoint for the storage account, then turn
	// off public network access for it.
	patch := fmt.Sprintf(
		`{"spec": {"storage": {"azure": {"networkAccess": {"type": "%s", "internal": {"vnetName": "%s", "subnetName": "%s"}}}}}}`,
		imageregistryv1.AzureNetworkAccessTypeInternal, vnetName, subnetName,
	)
	if _, err = te.Client().Configs().Patch(
		ctx,
		defaults.ImageRegistryResourceName,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	); err != nil {
		t.Errorf("unable to patch image registry custom resource: %#v", err)
	}

	azureConfig := &imageregistryv1.ImageRegistryConfigStorageAzure{}
	err = wait.PollUntilContextTimeout(ctx, time.Second, framework.AsyncOperationTimeout, true,
		func(ctx context.Context) (stop bool, err error) {
			cr, err := te.Client().Configs().Get(
				ctx,
				defaults.ImageRegistryResourceName,
				metav1.GetOptions{},
			)
			if err != nil {
				t.Logf(
					"unable to get custom resource %s/%s: %#v",
					defaults.ImageRegistryOperatorNamespace,
					defaults.ImageRegistryResourceName,
					err,
				)
				return false, nil
			}
			if cr.Spec.Storage.Azure.NetworkAccess == nil || cr.Spec.Storage.Azure.NetworkAccess.Type == imageregistryv1.AzureNetworkAccessTypeExternal {
				t.Logf("Private storage account not requested")
				// no point in keeping waiting
				return true, nil
			}
			if cr.Spec.Storage.Azure.NetworkAccess.Internal == nil || cr.Spec.Storage.Azure.NetworkAccess.Internal.PrivateEndpointName == "" {
				// operator has not yet set the name - keep waiting
				return false, nil
			}
			azureConfig = cr.Spec.Storage.Azure
			return true, nil
		},
	)

	if err != nil {
		t.Fatalf("failed to poll CR: %q", err)
	}

	if azureConfig.NetworkAccess.Internal.VNetName != vnetName {
		t.Fatalf(
			"vnet name differs from the one set. want %q but got %q.",
			vnetName, azureConfig.NetworkAccess.Internal.VNetName,
		)
	}
	if azureConfig.NetworkAccess.Internal.SubnetName != subnetName {
		t.Fatalf(
			"subnet name differs from the one set. want %q but got %q.",
			subnetName, azureConfig.NetworkAccess.Internal.SubnetName,
		)
	}

	// asserts that a private endpoint was created in Azure
	environment, err := getEnvironmentByName(azureConfig.CloudName)
	if err != nil {
		t.Fatalf("failed to get environment: %q", err)
	}
	cfg, err := azure.GetConfig(mockLister.Secrets, mockLister.Infrastructures)
	if err != nil {
		t.Fatalf("failed to get config: %q", err)
	}
	azclient, err := azureclient.New(&azureclient.Options{
		Environment:    environment,
		TenantID:       cfg.TenantID,
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		SubscriptionID: cfg.SubscriptionID,
	})
	if err != nil {
		t.Fatalf("failed to get new azure client: %q", err)
	}
	exists, err := azclient.PrivateEndpointExists(ctx, cfg.ResourceGroup, azureConfig.NetworkAccess.Internal.PrivateEndpointName)
	if err != nil {
		t.Fatalf("failed to check if private endpoint exists: %q", err)
	}
	if !exists {
		t.Fatal("private endpoint was not created")
	}

	isPrivate := azclient.IsStorageAccountPrivate(ctx, cfg.ResourceGroup, azureConfig.AccountName)
	if !isPrivate {
		t.Fatal("storage account network was not made private")
	}

	// set registry to "Removed" and assert that the private endpoint was
	// deleted

	if _, err := te.Client().Configs().Patch(
		ctx,
		defaults.ImageRegistryResourceName,
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:    "replace",
				Path:  "/spec/managementState",
				Value: operatorv1.Removed,
			},
		}),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("unable to switch to removed state: %s", err)
	}

	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, framework.AsyncOperationTimeout, false,
		func(ctx context.Context) (stop bool, err error) {
			cr, err := te.Client().Configs().Get(
				ctx, defaults.ImageRegistryResourceName, metav1.GetOptions{},
			)
			if err != nil {
				return false, err
			}

			conds := framework.GetImageRegistryConditions(cr)
			t.Logf("image registry: %s", conds)
			return conds.Available.IsTrue() && conds.Available.Reason() == "Removed" &&
				conds.Progressing.IsFalse() && conds.Progressing.Reason() == "Removed" &&
				conds.Degraded.IsFalse() &&
				conds.Removed.IsTrue(), nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	exists, err = azclient.PrivateEndpointExists(ctx, cfg.ResourceGroup, azureConfig.NetworkAccess.Internal.PrivateEndpointName)
	if err != nil {
		t.Fatalf("failed to check if private endpoint exists: %q", err)
	}
	if exists {
		t.Fatal("private endpoint was not deleted")
	}
}

func TestPrivateStorageAccountVNetSubnetDiscovery(t *testing.T) {
	ctx := context.Background()

	kcfg, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("Error building kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kcfg)
	if err != nil {
		t.Fatalf("unable to create mock lister: %v", err)
	}

	mockLister, err := newMockLister.GetListers()
	if err != nil {
		t.Fatalf("unable to get listers from mock lister: %v", err)
	}

	infra, err := util.GetInfrastructure(mockLister.StorageListers.Infrastructures)
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.AzurePlatformType {
		t.Skip("skipping on non-Azure platform")
	}

	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	framework.DeployImageRegistry(te, nil)
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsSet(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	patch := fmt.Sprintf(
		`{"spec": {"storage": {"azure": {"networkAccess": {"type": "%s"}}}}}`,
		imageregistryv1.AzureNetworkAccessTypeInternal,
	)
	if _, err = te.Client().Configs().Patch(
		ctx,
		defaults.ImageRegistryResourceName,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	); err != nil {
		t.Errorf("unable to patch image registry custom resource: %#v", err)
	}

	azureConfig := &imageregistryv1.ImageRegistryConfigStorageAzure{}
	err = wait.PollUntilContextTimeout(ctx, time.Second, framework.AsyncOperationTimeout, true,
		func(ctx context.Context) (stop bool, err error) {
			cr, err := te.Client().Configs().Get(
				ctx,
				defaults.ImageRegistryResourceName,
				metav1.GetOptions{},
			)
			if err != nil {
				t.Logf(
					"unable to get custom resource %s/%s: %#v",
					defaults.ImageRegistryOperatorNamespace,
					defaults.ImageRegistryResourceName,
					err,
				)
				return false, nil
			}
			if cr.Spec.Storage.Azure.NetworkAccess.Internal == nil || cr.Spec.Storage.Azure.NetworkAccess.Internal.PrivateEndpointName == "" {
				// operator has not yet set the name - keep waiting
				return false, nil
			}
			azureConfig = cr.Spec.Storage.Azure
			t.Logf("PrivateEndpointName is %q", azureConfig.NetworkAccess.Internal.PrivateEndpointName)
			return true, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if azureConfig.NetworkAccess.Internal.VNetName == "" {
		t.Fatal("expected vnet name to be set in operator config but it was empty")
	}
	if azureConfig.NetworkAccess.Internal.SubnetName == "" {
		t.Fatal("expected subnet name to be set in operator config but it was empty")
	}
}

func getEnvironmentByName(name string) (autorestazure.Environment, error) {
	if name == "" {
		return autorestazure.PublicCloud, nil
	}
	return autorestazure.EnvironmentFromName(name)
}
