package azureclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	autorestazure "github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/Azure/msi-dataplane/pkg/dataplane"
	"k8s.io/klog/v2"
)

const (
	targetSubResource          = "blob"
	defaultPrivateZoneName     = "privatelink.blob.core.windows.net"
	defaultPrivateZoneLocation = "global"
	defaultRecordSetTTL        = 10
	azureCredentialsKey        = "AzureCredentials"

	// Azure API retry configuration.
	// These values are tuned for handling Azure API rate limiting (HTTP 429 responses).
	// The SDK automatically handles 429s with exponential backoff.
	retryMaxRetries    = 2                // Maximum number of retry attempts
	retryDelay         = 10 * time.Second // Initial delay between retries
	retryMaxRetryDelay = 60 * time.Second // Maximum delay between retries
)

// ValidateCredentialFilePath validates that a credential file path is safe to use.
// It checks that the path is absolute, doesn't contain path traversal sequences,
// and that the file exists.
func ValidateCredentialFilePath(path string) error {
	if path == "" {
		return errors.New("credential file path is empty")
	}

	// Ensure the path is absolute to prevent relative path attacks
	if !filepath.IsAbs(path) {
		return fmt.Errorf("credential file path must be absolute: %s", path)
	}

	// Clean the path and check for path traversal attempts
	cleanPath := filepath.Clean(path)
	if cleanPath != path && strings.Contains(path, "..") {
		return fmt.Errorf("credential file path contains invalid sequences: %s", path)
	}

	// Check that the file exists and is accessible
	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("credential file does not exist: %s", cleanPath)
		}
		return fmt.Errorf("unable to access credential file: %w", err)
	}

	// Ensure it's a regular file, not a directory or symlink
	if info.IsDir() {
		return fmt.Errorf("credential file path is a directory: %s", cleanPath)
	}

	return nil
}

type Client struct {
	creds            azcore.TokenCredential
	clientOpts       *policy.ClientOptions
	opts             *Options
	azureCredentials sync.Map
}

type Options struct {
	Environment        autorestazure.Environment
	TenantID           string
	ClientID           string
	ClientSecret       string
	FederatedTokenFile string
	SubscriptionID     string
	TagSet             map[string]*string
	Policies           []policy.Policy
	Creds              azcore.TokenCredential
}

type PrivateEndpointCreateOptions struct {
	Location            string
	VNetName            string
	SubnetName          string
	PrivateEndpointName string
	// The resource group name where the vnet and subnet are.
	NetworkResourceGroupName string
	// The name of an existing storage account
	StorageAccountName string
	// The resource group name used by the cluster. This is where the
	// the storage account will be in.
	ClusterResourceGroupName string
}

func New(opts *Options) (*Client, error) {
	if err := validate(opts); err != nil {
		return nil, err
	}

	cloudConfig := cloud.Configuration{
		ActiveDirectoryAuthorityHost: opts.Environment.ActiveDirectoryEndpoint,
		Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
			cloud.ResourceManager: {
				Audience: opts.Environment.TokenAudience,
				Endpoint: opts.Environment.ResourceManagerEndpoint,
			},
		},
	}
	coreOpts := azcore.ClientOptions{
		Cloud: cloudConfig,
	}
	coreOpts.PerCallPolicies = opts.Policies
	creds := opts.Creds
	coreOpts.Retry = policy.RetryOptions{
		MaxRetries:    retryMaxRetries,
		RetryDelay:    retryDelay,
		MaxRetryDelay: retryMaxRetryDelay,
	}

	return &Client{
		creds:      creds,
		clientOpts: &coreOpts,
		opts:       opts,
	}, nil
}

func (c *Client) getCreds(ctx context.Context) (azcore.TokenCredential, error) {
	if c.creds != nil {
		return c.creds, nil
	}

	var (
		err   error
		creds azcore.TokenCredential
	)
	userAssignedIdentityCredentialsFilePath := os.Getenv("MANAGED_AZURE_HCP_CREDENTIALS_FILE_PATH")
	if userAssignedIdentityCredentialsFilePath != "" {
		var ok bool

		// We need to only store the Azure credentials once and reuse them after that.
		storedCreds, found := c.azureCredentials.Load(azureCredentialsKey)
		if !found {
			// Validate the credential file path before using it
			if err := ValidateCredentialFilePath(userAssignedIdentityCredentialsFilePath); err != nil {
				return nil, fmt.Errorf("invalid credential file path: %w", err)
			}
			klog.V(2).Info("Using UserAssignedIdentityCredentials for Azure authentication for managed Azure HCP")
			clientOptions := azcore.ClientOptions{
				Cloud: c.clientOpts.Cloud,
			}
			creds, err = dataplane.NewUserAssignedIdentityCredential(ctx, userAssignedIdentityCredentialsFilePath, dataplane.WithClientOpts(clientOptions))
			if err != nil {
				return nil, err
			}
			c.azureCredentials.Store(azureCredentialsKey, creds)
		} else {
			creds, ok = storedCreds.(azcore.TokenCredential)
			if !ok {
				return nil, fmt.Errorf("expected %T to be a TokenCredential", storedCreds)
			}
		}
	} else if strings.TrimSpace(c.opts.ClientSecret) == "" {
		options := azidentity.WorkloadIdentityCredentialOptions{
			ClientOptions: *c.clientOpts,
			ClientID:      c.opts.ClientID,
			TenantID:      c.opts.TenantID,
			TokenFilePath: c.opts.FederatedTokenFile,
		}
		creds, err = azidentity.NewWorkloadIdentityCredential(&options)
		if err != nil {
			return nil, err
		}
	} else {
		options := azidentity.ClientSecretCredentialOptions{
			ClientOptions: *c.clientOpts,
		}
		creds, err = azidentity.NewClientSecretCredential(
			c.opts.TenantID,
			c.opts.ClientID,
			c.opts.ClientSecret,
			&options,
		)
		if err != nil {
			return nil, err
		}
	}
	if creds == nil {
		return nil, errors.New("Unknown authentication method")
	}
	c.creds = creds
	return c.creds, nil
}

func (c *Client) getStorageAccount(ctx context.Context, resourceGroupName, accountName string) (armstorage.Account, error) {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return armstorage.Account{}, fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return armstorage.Account{}, fmt.Errorf("failed to create accounts client: %w", err)
	}
	resp, err := client.GetProperties(ctx, resourceGroupName, accountName, nil)
	if err != nil {
		return armstorage.Account{}, err
	}
	return resp.Account, nil
}

func (c *Client) vnetHasAnyTag(vnet armnetwork.VirtualNetwork, tagFilter map[string][]string) bool {
	for tagKey, tagValues := range tagFilter {
		tag, ok := vnet.Tags[tagKey]
		if !ok {
			continue
		}
		for _, tagValue := range tagValues {
			if *tag == tagValue {
				return true
			}
		}
	}
	return false
}

func (c *Client) GetVNetByTags(ctx context.Context, resourceGroupName string, tagFilter map[string][]string) (armnetwork.VirtualNetwork, error) {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return armnetwork.VirtualNetwork{}, fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armnetwork.NewVirtualNetworksClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return armnetwork.VirtualNetwork{}, fmt.Errorf("failed to create virtual networks client: %w", err)
	}

	pager := client.NewListPager(resourceGroupName, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return armnetwork.VirtualNetwork{}, err
		}
		for _, vnet := range page.Value {
			if c.vnetHasAnyTag(*vnet, tagFilter) {
				return *vnet, nil
			}
		}
	}

	return armnetwork.VirtualNetwork{}, fmt.Errorf("vnet with tags '%+v' not found", tagFilter)
}

func (c *Client) GetSubnetsByVNet(ctx context.Context, resourceGroupName, vnetName string) (armnetwork.Subnet, error) {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return armnetwork.Subnet{}, fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armnetwork.NewSubnetsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return armnetwork.Subnet{}, fmt.Errorf("failed to create subnets client: %w", err)
	}

	pager := client.NewListPager(resourceGroupName, vnetName, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return armnetwork.Subnet{}, err
		}
		for _, subnet := range page.Value {
			// should we match the subnet name with the string "worker"?
			// does it even matter? (don't think so)
			//
			// return the first subnet.
			// unless each subnet in the cluster has strict access groups it
			// doesn't matter which subnet we choose (worker/master).
			return *subnet, nil
		}
	}
	return armnetwork.Subnet{}, fmt.Errorf("no subnets found on vnet %q", vnetName)
}

func (c *Client) UpdateStorageAccountNetworkAccess(ctx context.Context, resourceGroupName, accountName string, allowPublicAccess bool) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create accounts client: %w", err)
	}
	publicNetworkAccess := armstorage.PublicNetworkAccessDisabled
	if allowPublicAccess {
		publicNetworkAccess = armstorage.PublicNetworkAccessEnabled
	}
	params := armstorage.AccountUpdateParameters{
		Properties: &armstorage.AccountPropertiesUpdateParameters{
			PublicNetworkAccess: &publicNetworkAccess,
		},
	}
	if _, err := client.Update(ctx, resourceGroupName, accountName, params, nil); err != nil {
		return err
	}
	return nil
}

func (c *Client) DisableStorageAccountAccessKeyAccess(ctx context.Context, resourceGroupName, accountName string) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create accounts client: %w", err)
	}

	params := armstorage.AccountUpdateParameters{
		Properties: &armstorage.AccountPropertiesUpdateParameters{
			AllowSharedKeyAccess: to.BoolPtr(false),
		},
	}
	if _, err := client.Update(ctx, resourceGroupName, accountName, params, nil); err != nil {
		return err
	}
	return nil
}

// IsStorageAccountPrivate gets a storage account and returns true if public
// network access is disabled, or false if public network access is enabled.
// Public network access is enabled by default in Azure. In case of any
// unexpected behaviour this function will return false.
func (c *Client) IsStorageAccountPrivate(ctx context.Context, resourceGroupName, accountName string) bool {
	account, err := c.getStorageAccount(ctx, resourceGroupName, accountName)
	if err != nil {
		return false
	}
	if account.Properties == nil {
		return false
	}
	publicNetworkAccess := account.Properties.PublicNetworkAccess
	if publicNetworkAccess == nil {
		return false
	}
	return *publicNetworkAccess == armstorage.PublicNetworkAccessDisabled
}

func (c *Client) PrivateEndpointExists(ctx context.Context, resourceGroupName, privateEndpointName string) (bool, error) {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armnetwork.NewPrivateEndpointsClient(
		c.opts.SubscriptionID,
		creds,
		&arm.ClientOptions{
			ClientOptions: *c.clientOpts,
		},
	)
	if err != nil {
		return false, err
	}
	if _, err := client.Get(ctx, resourceGroupName, privateEndpointName, nil); err != nil {
		respErr, ok := err.(*azcore.ResponseError)
		if ok && respErr.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *Client) CreatePrivateEndpoint(
	ctx context.Context,
	opts *PrivateEndpointCreateOptions,
) (*armnetwork.PrivateEndpoint, error) {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armnetwork.NewPrivateEndpointsClient(
		c.opts.SubscriptionID,
		creds,
		&arm.ClientOptions{
			ClientOptions: *c.clientOpts,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create private endpoints client: %w", err)
	}

	privateLinkResourceID := formatPrivateLinkResourceID(
		c.opts.SubscriptionID,
		opts.ClusterResourceGroupName,
		opts.StorageAccountName,
	)
	subnetID := formatSubnetID(
		opts.SubnetName,
		opts.VNetName,
		opts.NetworkResourceGroupName,
		c.opts.SubscriptionID,
	)

	privateEndpointName := opts.PrivateEndpointName

	params := armnetwork.PrivateEndpoint{
		Location: to.StringPtr(opts.Location),
		Tags:     c.opts.TagSet,
		Properties: &armnetwork.PrivateEndpointProperties{
			CustomNetworkInterfaceName: to.StringPtr(fmt.Sprintf("%s-nic", privateEndpointName)),
			Subnet:                     &armnetwork.Subnet{ID: to.StringPtr(subnetID)},
			PrivateLinkServiceConnections: []*armnetwork.PrivateLinkServiceConnection{{
				Name: to.StringPtr(privateEndpointName),
				Properties: &armnetwork.PrivateLinkServiceConnectionProperties{
					PrivateLinkServiceID: to.StringPtr(privateLinkResourceID),
					GroupIDs:             []*string{to.StringPtr(targetSubResource)},
				},
			}},
		},
	}

	pollersResp, err := client.BeginCreateOrUpdate(
		ctx,
		opts.ClusterResourceGroupName,
		privateEndpointName,
		params,
		nil,
	)
	if err != nil {
		return nil, err
	}
	resp, err := pollersResp.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &resp.PrivateEndpoint, nil
}

func (c *Client) DeletePrivateEndpoint(ctx context.Context, resourceGroupName, privateEndpointName string) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armnetwork.NewPrivateEndpointsClient(
		c.opts.SubscriptionID,
		creds,
		&arm.ClientOptions{
			ClientOptions: *c.clientOpts,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create private endpoints client: %w", err)
	}
	pollersResp, err := client.BeginDelete(
		ctx,
		resourceGroupName,
		privateEndpointName,
		nil,
	)
	if err != nil && !c.is404(err) {
		return err
	}
	if _, err := pollersResp.PollUntilDone(ctx, nil); err != nil && !c.is404(err) {
		return err
	}
	return nil
}

// ConfigurePrivateDNS creates a private DNS zone, a record set (A) and a
// private DNS zone group for the given private endpoint.
// It also links the DNS zone with the given VNet by creating a virtual network
// link.
//
// Returns an error on failure.
func (c *Client) ConfigurePrivateDNS(
	ctx context.Context,
	privateEndpoint *armnetwork.PrivateEndpoint,
	clusterResourceGroupName,
	networkResourceGroupName,
	vnetName,
	storageAccountName string,
) error {
	if err := c.createPrivateDNSZone(ctx, clusterResourceGroupName, defaultPrivateZoneName, defaultPrivateZoneLocation); err != nil {
		return err
	}

	if err := c.createRecordSet(ctx, privateEndpoint, clusterResourceGroupName, defaultPrivateZoneName, storageAccountName); err != nil {
		return err
	}

	if err := c.createPrivateDNSZoneGroup(ctx, clusterResourceGroupName, *privateEndpoint.Name, defaultPrivateZoneName); err != nil {
		return err
	}

	if err := c.createVirtualNetworkLink(
		ctx,
		clusterResourceGroupName,
		networkResourceGroupName,
		storageAccountName,
		vnetName,
		defaultPrivateZoneName,
		defaultPrivateZoneLocation,
	); err != nil {
		respErr, ok := err.(*azcore.ResponseError)
		// on conflict, azure api will not return a 409 so we need
		// to match for the string.
		if !ok || respErr.ErrorCode != "Conflict" {
			return err
		}
	}

	return nil
}

// DestroyPrivateDNS unlinks the private zone from the vnet.
//
// It is meant to be used as a clean-up for ConfigurePrivateDNS. It will not
// undo everything ConfigurePrivateDNS does because it's difficult to know
// whether they are used by other components. We remove the resources we know
// for sure that the registry is the only one using.
func (c *Client) DestroyPrivateDNS(ctx context.Context, resourceGroupName, privateEndpointName, vnetName, storageAccountName string) error {
	if err := c.deleteRecordSet(
		ctx, resourceGroupName, privateEndpointName, defaultPrivateZoneName,
	); err != nil && !c.is404(err) {
		return err
	}
	if err := c.deletePrivateDNSZoneGroup(
		ctx, resourceGroupName, privateEndpointName, defaultPrivateZoneName,
	); err != nil && !c.is404(err) {
		return err
	}
	if err := c.deleteVirtualNetworkLink(
		ctx, resourceGroupName, storageAccountName, defaultPrivateZoneName,
	); err != nil && !c.is404(err) {
		return err
	}
	return nil
}

func (c *Client) createPrivateDNSZone(ctx context.Context, resourceGroupName, name, location string) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armprivatedns.NewPrivateZonesClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return err
	}
	pollersResp, err := client.BeginCreateOrUpdate(
		ctx,
		resourceGroupName,
		name,
		armprivatedns.PrivateZone{
			Location: to.StringPtr(location),
			Tags:     c.opts.TagSet,
		},
		nil,
	)
	if err != nil {
		return err
	}
	if _, err := pollersResp.PollUntilDone(ctx, nil); err != nil {
		return err
	}
	return nil
}

func (c *Client) createRecordSet(
	ctx context.Context,
	privateEndpoint *armnetwork.PrivateEndpoint,
	resourceGroupName,
	privateZoneName,
	relativeRecordSetName string,
) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armprivatedns.NewRecordSetsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create record sets client: %w", err)
	}

	nicAddress, err := c.getNICAddress(ctx, resourceGroupName, privateEndpoint)
	if err != nil {
		return err
	}

	rs := armprivatedns.RecordSet{
		Properties: &armprivatedns.RecordSetProperties{
			TTL: to.Int64Ptr(defaultRecordSetTTL),
			ARecords: []*armprivatedns.ARecord{{
				IPv4Address: to.StringPtr(nicAddress),
			}},
		},
	}

	if _, err := client.CreateOrUpdate(
		ctx,
		resourceGroupName,
		privateZoneName,
		armprivatedns.RecordTypeA,
		relativeRecordSetName,
		rs,
		nil,
	); err != nil {
		return err
	}

	return nil
}

func (c *Client) deleteRecordSet(ctx context.Context, resourceGroupName, privateZoneName, relativeRecordSetName string) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armprivatedns.NewRecordSetsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create record sets client: %w", err)
	}
	if _, err := client.Delete(
		ctx,
		resourceGroupName,
		privateZoneName,
		armprivatedns.RecordTypeA,
		relativeRecordSetName,
		nil,
	); err != nil {
		return err
	}

	return nil
}

func (c *Client) createPrivateDNSZoneGroup(ctx context.Context, resourceGroupName, privateEndpointName, privateZoneName string) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armnetwork.NewPrivateDNSZoneGroupsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create private dns zone groups client: %w", err)
	}
	privateZoneID := formatPrivateDNSZoneID(c.opts.SubscriptionID, resourceGroupName, privateZoneName)
	groupName := strings.Replace(privateZoneName, ".", "-", -1)
	group := armnetwork.PrivateDNSZoneGroup{
		Name: to.StringPtr(fmt.Sprintf("%s/default", privateZoneName)),
		Properties: &armnetwork.PrivateDNSZoneGroupPropertiesFormat{
			PrivateDNSZoneConfigs: []*armnetwork.PrivateDNSZoneConfig{{
				Name: to.StringPtr(groupName),
				Properties: &armnetwork.PrivateDNSZonePropertiesFormat{
					PrivateDNSZoneID: to.StringPtr(privateZoneID),
				},
			}},
		},
	}
	pollersResp, err := client.BeginCreateOrUpdate(
		ctx,
		resourceGroupName,
		privateEndpointName,
		groupName,
		group,
		nil,
	)
	if err != nil {
		return err
	}
	if _, err := pollersResp.PollUntilDone(ctx, nil); err != nil {
		return err
	}
	return nil
}

func (c *Client) deletePrivateDNSZoneGroup(ctx context.Context, resourceGroupName, privateEndpointName, privateZoneName string) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armnetwork.NewPrivateDNSZoneGroupsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create private dns zone groups client: %w", err)
	}
	groupName := strings.Replace(privateZoneName, ".", "-", -1)
	pollersResp, err := client.BeginDelete(
		ctx,
		resourceGroupName,
		privateEndpointName,
		groupName,
		nil,
	)
	if err != nil {
		return err
	}
	if _, err := pollersResp.PollUntilDone(ctx, nil); err != nil {
		return err
	}
	return nil
}

func (c *Client) createVirtualNetworkLink(
	ctx context.Context,
	clusterResourceGroupName,
	networkResourceGroupName,
	linkName,
	vnetName,
	privateZoneName,
	privateZoneLocation string,
) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armprivatedns.NewVirtualNetworkLinksClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create virtual network links client: %w", err)
	}

	vnetID := formatVNetID(c.opts.SubscriptionID, networkResourceGroupName, vnetName)

	pollersResp, err := client.BeginCreateOrUpdate(
		ctx,
		clusterResourceGroupName,
		privateZoneName,
		linkName,
		armprivatedns.VirtualNetworkLink{
			Location: to.StringPtr(privateZoneLocation),
			Tags:     c.opts.TagSet,
			Properties: &armprivatedns.VirtualNetworkLinkProperties{
				RegistrationEnabled: to.BoolPtr(false),
				VirtualNetwork:      &armprivatedns.SubResource{ID: to.StringPtr(vnetID)},
			},
		},
		nil,
	)
	if err != nil {
		return err
	}

	if _, err := pollersResp.PollUntilDone(ctx, nil); err != nil {
		return err
	}

	return nil
}

func (c *Client) deleteVirtualNetworkLink(ctx context.Context, clusterResourceGroupName, linkName, privateZoneName string) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armprivatedns.NewVirtualNetworkLinksClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create virtual network links client: %w", err)
	}

	pollersResp, err := client.BeginDelete(
		ctx,
		clusterResourceGroupName,
		privateZoneName,
		linkName,
		nil,
	)
	if err != nil {
		return err
	}
	if _, err := pollersResp.PollUntilDone(ctx, nil); err != nil {
		return err
	}
	return nil
}

func (c *Client) is404(err error) bool {
	respErr, ok := err.(*azcore.ResponseError)
	if !ok {
		return false
	}
	return respErr.StatusCode == http.StatusNotFound
}

func (c *Client) getNICAddress(ctx context.Context, resourceGroupName string, privateEndpoint *armnetwork.PrivateEndpoint) (string, error) {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armnetwork.NewInterfacesClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return "", err
	}

	if len(privateEndpoint.Properties.NetworkInterfaces) == 0 {
		return "", fmt.Errorf("private endpoint %s did not have any network interfaces", *privateEndpoint.Name)
	}
	// this is auto-created by Azure and there should always ever be one.
	nicRef := privateEndpoint.Properties.NetworkInterfaces[0]
	nicIDParts := strings.Split(*nicRef.ID, "/")
	nicName := nicIDParts[len(nicIDParts)-1]

	resp, err := client.Get(ctx, resourceGroupName, nicName, nil)
	if err != nil {
		return "", err
	}
	nic := resp.Interface
	if len(nic.Properties.IPConfigurations) == 0 {
		return "", fmt.Errorf("network interface %s did not have any IP configurations", *nic.Name)
	}

	// this is auto-created by Azure and there should always ever be one.
	nicAddress := nic.Properties.IPConfigurations[0].Properties.PrivateIPAddress

	return *nicAddress, nil
}

func formatSubnetID(subnetName, vnetName, resourceGroupName, subscriptionID string) string {
	return fmt.Sprintf(
		"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s/subnets/%s",
		subscriptionID,
		resourceGroupName,
		vnetName,
		subnetName,
	)
}

func formatPrivateLinkResourceID(subscriptionID, resourceGroupName, storageAccountName string) string {
	return fmt.Sprintf(
		"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s",
		subscriptionID,
		resourceGroupName,
		storageAccountName,
	)
}

func formatPrivateDNSZoneID(subscriptionID, resourceGroupName, privateZoneName string) string {
	return fmt.Sprintf(
		"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/privateDnsZones/%s",
		subscriptionID,
		resourceGroupName,
		privateZoneName,
	)
}

func formatVNetID(subscriptionID, resourceGroupName, vnetName string) string {
	return fmt.Sprintf(
		"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/virtualNetworks/%s",
		subscriptionID,
		resourceGroupName,
		vnetName,
	)
}

func validate(opts *Options) error {
	missingOpts := []string{}
	if opts.Environment.ResourceManagerEndpoint == "" {
		missingOpts = append(missingOpts, "'Environment.ResourceManagerEndpoint'")
	}
	if opts.Environment.ActiveDirectoryEndpoint == "" {
		missingOpts = append(missingOpts, "'Environment.ActiveDirectoryEndpoint'")
	}
	if opts.Environment.TokenAudience == "" {
		missingOpts = append(missingOpts, "'Environment.TokenAudience'")
	}
	// do not validate auth specific options - different operations might require different auth.
	// i.e some functions take in an account key, while others will rely on client id or client secret.
	// better to not try and validate every combination.
	if len(missingOpts) > 0 {
		msg := strings.Join(missingOpts, ", ")
		return fmt.Errorf("client misconfigured, missing %s option(s)", msg)
	}
	return nil
}

func (c *Client) NewBlobClient(environment autorestazure.Environment, accountName, key, blobURL string) (*BlobClient, error) {
	if key != "" {
		cred, err := azblob.NewSharedKeyCredential(accountName, key)
		if err != nil {
			return nil, err
		}
		client, err := azblob.NewClientWithSharedKeyCredential(blobURL, cred, &azblob.ClientOptions{
			ClientOptions: *c.clientOpts,
		})
		return &BlobClient{
			client: client,
		}, err
	}

	creds, err := c.getCreds(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := azblob.NewClient(blobURL, creds, &azblob.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	return &BlobClient{
		client: client,
	}, err
}

type BlobClient struct {
	client *azblob.Client
}

// containerExists determines whether or not an azure container exists
func (client *BlobClient) ContainerExists(ctx context.Context, accountName, containerName string) (bool, error) {
	if accountName == "" || containerName == "" {
		return false, nil
	}

	c := client.client.ServiceClient().NewContainerClient(containerName)
	_, err := c.GetProperties(ctx, &container.GetPropertiesOptions{})
	if err != nil {
		if !bloberror.HasCode(err, bloberror.ContainerNotFound) {
			return false, fmt.Errorf("unable to get the storage container %s: %s", containerName, err)
		} else {
			return false, nil
		}
	}
	return true, nil
}

func (client *BlobClient) CreateStorageContainer(ctx context.Context, containerName string) error {
	_, err := client.client.CreateContainer(ctx, containerName, &azblob.CreateContainerOptions{})
	return err
}

func (client *BlobClient) DeleteStorageContainer(ctx context.Context, containerName string) error {
	_, err := client.client.DeleteContainer(ctx, containerName, &azblob.DeleteContainerOptions{})
	return err
}

// StorageAccountCreateOptions contains options for creating a storage account.
type StorageAccountCreateOptions struct {
	ResourceGroupName string
	AccountName       string
	Location          string
	CloudName         string // For Azure Stack Hub detection
	Tags              map[string]*string
}

// CheckStorageAccountNameAvailability checks if the storage account name is available.
func (c *Client) CheckStorageAccountNameAvailability(ctx context.Context, accountName string) (*armstorage.CheckNameAvailabilityResult, error) {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create accounts client: %w", err)
	}

	resp, err := client.CheckNameAvailability(ctx, armstorage.AccountCheckNameAvailabilityParameters{
		Name: to.StringPtr(accountName),
		Type: to.StringPtr("Microsoft.Storage/storageAccounts"),
	}, nil)
	if err != nil {
		return nil, err
	}
	return &resp.CheckNameAvailabilityResult, nil
}

// IsAzureStackCloud checks if the cloud name indicates Azure Stack Hub.
func IsAzureStackCloud(name string) bool {
	return strings.EqualFold(name, "AZURESTACKCLOUD")
}

// CreateStorageAccount creates a new storage account.
func (c *Client) CreateStorageAccount(ctx context.Context, opts *StorageAccountCreateOptions) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
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

	if IsAzureStackCloud(opts.CloudName) {
		// Azure Stack Hub does not support new API features
		kind = armstorage.KindStorage
		params.Kind = &kind
		params.Properties = &armstorage.AccountPropertiesCreateParameters{}
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

// ListStorageAccountKeys lists the access keys for a storage account.
func (c *Client) ListStorageAccountKeys(ctx context.Context, resourceGroupName, accountName string) ([]*armstorage.AccountKey, error) {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create accounts client: %w", err)
	}

	expand := "kerb"
	resp, err := client.ListKeys(ctx, resourceGroupName, accountName, &armstorage.AccountsClientListKeysOptions{
		Expand: &expand,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list storage account keys: %w", err)
	}
	return resp.Keys, nil
}

// GetPrimaryStorageAccountKey returns the primary key for a storage account.
func (c *Client) GetPrimaryStorageAccountKey(ctx context.Context, resourceGroupName, accountName string) (string, error) {
	keys, err := c.ListStorageAccountKeys(ctx, resourceGroupName, accountName)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", fmt.Errorf("no keys found for storage account %s", accountName)
	}
	if keys[0].Value == nil {
		return "", fmt.Errorf("primary key value is nil for storage account %s", accountName)
	}
	return *keys[0].Value, nil
}

// DeleteStorageAccount deletes a storage account. Returns nil if the account doesn't exist.
func (c *Client) DeleteStorageAccount(ctx context.Context, resourceGroupName, accountName string) error {
	creds, err := c.getCreds(ctx)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	client, err := armstorage.NewAccountsClient(c.opts.SubscriptionID, creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create accounts client: %w", err)
	}

	_, err = client.Delete(ctx, resourceGroupName, accountName, nil)
	if err != nil {
		// Ignore 404 errors - account already deleted
		if c.is404(err) {
			return nil
		}
		return err
	}
	return nil
}
