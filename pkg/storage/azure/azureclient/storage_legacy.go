package azureclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/storage/mgmt/2019-06-01/storage"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/to"
	"k8s.io/klog/v2"
)

// legacyStorageClient implements StorageAccountClient using Track 1 SDK.
// This is required for Azure Stack Hub which only supports API version 2019-06-01.
type legacyStorageClient struct {
	base *Client
}

func newLegacyStorageClient(base *Client) *legacyStorageClient {
	return &legacyStorageClient{base: base}
}

func (c *legacyStorageClient) CheckNameAvailability(ctx context.Context, accountName string) (bool, error) {
	client, err := c.getAccountsClient(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to create accounts client: %w", err)
	}

	result, err := client.CheckNameAvailability(ctx, storage.AccountCheckNameAvailabilityParameters{
		Name: to.StringPtr(accountName),
		Type: to.StringPtr("Microsoft.Storage/storageAccounts"),
	})
	if err != nil {
		return false, fmt.Errorf("failed to check name availability: %w", err)
	}

	// Return true if name is TAKEN (not available = account exists)
	if result.NameAvailable != nil && *result.NameAvailable {
		return false, nil // Name available = account does NOT exist
	}
	return true, nil // Name not available = account exists
}

func (c *legacyStorageClient) Create(ctx context.Context, opts *StorageAccountCreateOptions) error {
	client, err := c.getAccountsClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create accounts client: %w", err)
	}

	klog.Infof("attempt to create azure storage account %s (resourceGroup=%q, location=%q)...", opts.AccountName, opts.ResourceGroupName, opts.Location)

	// Azure Stack Hub: use KindStorage (v1), not KindStorageV2
	params := storage.AccountCreateParameters{
		Kind:     storage.Storage,
		Location: to.StringPtr(opts.Location),
		Sku:      &storage.Sku{Name: storage.StandardLRS},
		Tags:     opts.Tags,
	}

	future, err := client.Create(ctx, opts.ResourceGroupName, opts.AccountName, params)
	if err != nil {
		return fmt.Errorf("failed to initiate storage account creation: %w", err)
	}

	if err := future.WaitForCompletionRef(ctx, client.Client); err != nil {
		return fmt.Errorf("failed waiting for storage account creation: %w", err)
	}

	klog.Infof("azure storage account %s has been created", opts.AccountName)
	return nil
}

func (c *legacyStorageClient) Delete(ctx context.Context, resourceGroup, accountName string) error {
	client, err := c.getAccountsClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create accounts client: %w", err)
	}

	_, err = client.Delete(ctx, resourceGroup, accountName)
	if err != nil {
		// Ignore 404 - account already deleted
		if detailedErr, ok := err.(autorest.DetailedError); ok {
			if detailedErr.StatusCode == http.StatusNotFound {
				return nil
			}
		}
		return fmt.Errorf("failed to delete storage account: %w", err)
	}
	return nil
}

func (c *legacyStorageClient) GetPrimaryKey(ctx context.Context, resourceGroup, accountName string) (string, error) {
	client, err := c.getAccountsClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create accounts client: %w", err)
	}

	keys, err := client.ListKeys(ctx, resourceGroup, accountName, storage.Kerb)
	if err != nil {
		return "", fmt.Errorf("failed to list storage account keys: %w", err)
	}

	if keys.Keys == nil || len(*keys.Keys) == 0 {
		return "", fmt.Errorf("no keys returned for storage account %s", accountName)
	}

	firstKey := (*keys.Keys)[0]
	if firstKey.Value == nil {
		return "", fmt.Errorf("primary key value is nil for storage account %s", accountName)
	}

	return *firstKey.Value, nil
}

// getAccountsClient creates a Track 1 SDK accounts client with proper auth.
func (c *legacyStorageClient) getAccountsClient(ctx context.Context) (storage.AccountsClient, error) {
	client := storage.NewAccountsClientWithBaseURI(c.base.opts.Environment.ResourceManagerEndpoint, c.base.opts.SubscriptionID)

	// Configure authorizer using client credentials
	oauthConfig, err := adal.NewOAuthConfig(c.base.opts.Environment.ActiveDirectoryEndpoint, c.base.opts.TenantID)
	if err != nil {
		return storage.AccountsClient{}, fmt.Errorf("failed to create OAuth config: %w", err)
	}

	// Use TokenAudience (not ResourceManagerEndpoint) as the OAuth resource.
	// For Azure Stack Hub, ResourceManagerEndpoint is the ARM API URL
	// (e.g., https://management.mtcazs.wwtatc.com) which is not registered
	// as a resource principal in Azure AD. TokenAudience contains the correct
	// audience for token requests (e.g., https://management.azure.com/).
	resource := strings.TrimSuffix(c.base.opts.Environment.TokenAudience, "/")

	var authorizer autorest.Authorizer
	if strings.TrimSpace(c.base.opts.ClientSecret) != "" {
		// Use client secret authentication
		spt, err := adal.NewServicePrincipalToken(*oauthConfig, c.base.opts.ClientID, c.base.opts.ClientSecret, resource)
		if err != nil {
			return storage.AccountsClient{}, fmt.Errorf("failed to create service principal token: %w", err)
		}
		authorizer = autorest.NewBearerAuthorizer(spt)
	} else {
		return storage.AccountsClient{}, fmt.Errorf("client secret is required for Azure Stack Hub authentication")
	}

	client.Authorizer = authorizer
	return client, nil
}
