package azureclient

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/go-autorest/autorest/to"
	"k8s.io/klog/v2"
)

// armStorageClient implements StorageAccountClient using Track 2 SDK.
// This is used for Azure public cloud.
type armStorageClient struct {
	base *Client
}

func newARMStorageClient(base *Client) *armStorageClient {
	return &armStorageClient{base: base}
}

func (c *armStorageClient) CheckNameAvailability(ctx context.Context, accountName string) (bool, error) {
	creds, err := c.base.getCreds(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.base.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.base.clientOpts,
	})
	if err != nil {
		return false, fmt.Errorf("failed to create accounts client: %w", err)
	}

	resp, err := client.CheckNameAvailability(ctx, armstorage.AccountCheckNameAvailabilityParameters{
		Name: to.StringPtr(accountName),
		Type: to.StringPtr("Microsoft.Storage/storageAccounts"),
	}, nil)
	if err != nil {
		return false, fmt.Errorf("failed to check name availability: %w", err)
	}

	// Return true if name is TAKEN (not available = account exists)
	if resp.NameAvailable != nil && *resp.NameAvailable {
		return false, nil // Name available = account does NOT exist
	}
	return true, nil // Name not available = account exists
}

func (c *armStorageClient) Create(ctx context.Context, opts *StorageAccountCreateOptions) error {
	creds, err := c.base.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.base.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.base.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create accounts client: %w", err)
	}

	klog.Infof("attempt to create azure storage account %s (resourceGroup=%q, location=%q)...", opts.AccountName, opts.ResourceGroupName, opts.Location)

	kind := armstorage.KindStorageV2
	skuName := armstorage.SKUNameStandardLRS
	minTLSVersion := armstorage.MinimumTLSVersionTLS12
	params := armstorage.AccountCreateParameters{
		Kind:     &kind,
		Location: to.StringPtr(opts.Location),
		SKU: &armstorage.SKU{
			Name: &skuName,
		},
		Properties: &armstorage.AccountPropertiesCreateParameters{
			EnableHTTPSTrafficOnly: to.BoolPtr(true),
			AllowBlobPublicAccess:  to.BoolPtr(false),
			MinimumTLSVersion:      &minTLSVersion,
		},
		Tags: opts.Tags,
	}

	poller, err := client.BeginCreate(ctx, opts.ResourceGroupName, opts.AccountName, params, nil)
	if err != nil {
		return fmt.Errorf("failed to start creating storage account: %w", err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to create storage account: %w", err)
	}

	klog.Infof("azure storage account %s has been created", opts.AccountName)
	return nil
}

func (c *armStorageClient) Delete(ctx context.Context, resourceGroup, accountName string) error {
	creds, err := c.base.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.base.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.base.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create accounts client: %w", err)
	}

	_, err = client.Delete(ctx, resourceGroup, accountName, nil)
	if err != nil {
		// Ignore 404 errors - account already deleted
		if c.base.is404(err) {
			return nil
		}
		return fmt.Errorf("failed to delete storage account: %w", err)
	}
	return nil
}

func (c *armStorageClient) GetPrimaryKey(ctx context.Context, resourceGroup, accountName string) (string, error) {
	creds, err := c.base.getCreds(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.base.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.base.clientOpts,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create accounts client: %w", err)
	}

	expand := "kerb"
	resp, err := client.ListKeys(ctx, resourceGroup, accountName, &armstorage.AccountsClientListKeysOptions{
		Expand: &expand,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list storage account keys: %w", err)
	}

	if len(resp.Keys) == 0 {
		return "", fmt.Errorf("no keys found for storage account %s", accountName)
	}
	if resp.Keys[0].Value == nil {
		return "", fmt.Errorf("primary key value is nil for storage account %s", accountName)
	}
	return *resp.Keys[0].Value, nil
}
