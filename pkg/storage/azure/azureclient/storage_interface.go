package azureclient

import "context"

// StorageAccountClient abstracts storage account management operations.
// Implementations exist for Azure public cloud (Track 2 SDK) and
// Azure Stack Hub (Track 1 SDK with 2019-06-01 API).
type StorageAccountClient interface {
	// CheckNameAvailability returns true if the account name is already taken.
	CheckNameAvailability(ctx context.Context, accountName string) (bool, error)

	// Create creates a new storage account with the given options.
	Create(ctx context.Context, opts *StorageAccountCreateOptions) error

	// Delete removes a storage account. Returns nil if account doesn't exist.
	Delete(ctx context.Context, resourceGroup, accountName string) error

	// GetPrimaryKey retrieves the primary access key for the storage account.
	GetPrimaryKey(ctx context.Context, resourceGroup, accountName string) (string, error)
}

// NewStorageAccountClient returns the appropriate implementation based on cloud type.
// For Azure Stack Hub, returns legacy SDK implementation with 2019-06-01 API.
// For all other clouds, returns Track 2 SDK implementation.
func NewStorageAccountClient(client *Client, cloudName string) StorageAccountClient {
	if IsAzureStackCloud(cloudName) {
		return newLegacyStorageClient(client)
	}
	return newARMStorageClient(client)
}
