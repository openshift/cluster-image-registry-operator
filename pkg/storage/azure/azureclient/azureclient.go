package azureclient

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

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
)

const (
	targetSubResource          = "blob"
	defaultPrivateZoneName     = "privatelink.blob.core.windows.net"
	defaultPrivateZoneLocation = "global"
	defaultRecordSetTTL        = 10
)

type Client struct {
	creds          azcore.TokenCredential
	clientOpts     *policy.ClientOptions
	tagset         map[string]*string
	subscriptionID string
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
	if creds == nil {
		var err error

		// MSI Override for ARO HCP
		msi := os.Getenv("AZURE_MSI_AUTHENTICATION")
		if msi == "true" {
			options := azidentity.ManagedIdentityCredentialOptions{
				ClientOptions: azcore.ClientOptions{
					Cloud: cloudConfig,
				},
			}
			creds, err = azidentity.NewManagedIdentityCredential(&options)
			if err != nil {
				return nil, err
			}
		} else if strings.TrimSpace(opts.ClientSecret) == "" {
			options := azidentity.WorkloadIdentityCredentialOptions{
				ClientOptions: coreOpts,
				ClientID:      opts.ClientID,
				TenantID:      opts.TenantID,
				TokenFilePath: opts.FederatedTokenFile,
			}
			creds, err = azidentity.NewWorkloadIdentityCredential(&options)
			if err != nil {
				return nil, err
			}
		} else {
			options := azidentity.ClientSecretCredentialOptions{
				ClientOptions: coreOpts,
			}
			creds, err = azidentity.NewClientSecretCredential(
				opts.TenantID,
				opts.ClientID,
				opts.ClientSecret,
				&options,
			)
			if err != nil {
				return nil, err
			}
		}
	}

	coreOpts.Retry = policy.RetryOptions{
		MaxRetries: -1, // try once
	}

	return &Client{
		creds:          creds,
		clientOpts:     &coreOpts,
		tagset:         opts.TagSet,
		subscriptionID: opts.SubscriptionID,
	}, nil
}

func (c *Client) getStorageAccount(ctx context.Context, resourceGroupName, accountName string) (armstorage.Account, error) {
	client, err := armstorage.NewAccountsClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return armstorage.Account{}, fmt.Errorf("failed to create accounts client: %q", err)
	}
	resp, err := client.GetProperties(ctx, resourceGroupName, accountName, nil)
	if err != nil {
		return armstorage.Account{}, err
	}
	return resp.Account, nil
}

func (c *Client) GetVNetByTag(ctx context.Context, resourceGroupName, tagKey string, tagValues ...string) (armnetwork.VirtualNetwork, error) {
	client, err := armnetwork.NewVirtualNetworksClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return armnetwork.VirtualNetwork{}, fmt.Errorf("failed to create accounts client: %q", err)
	}

	pager := client.NewListPager(resourceGroupName, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return armnetwork.VirtualNetwork{}, err
		}
		for _, vnet := range page.Value {
			tag, ok := vnet.Tags[tagKey]
			if !ok {
				continue
			}
			for _, tagValue := range tagValues {
				if *tag == tagValue {
					return *vnet, nil
				}
			}
		}
	}

	return armnetwork.VirtualNetwork{}, fmt.Errorf("vnet with tag '%s: %v' not found", tagKey, tagValues)
}

func (c *Client) GetSubnetsByVNet(ctx context.Context, resourceGroupName, vnetName string) (armnetwork.Subnet, error) {
	client, err := armnetwork.NewSubnetsClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return armnetwork.Subnet{}, fmt.Errorf("failed to create subnets client: %q", err)
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
	client, err := armstorage.NewAccountsClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create accounts client: %q", err)
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
	client, err := armstorage.NewAccountsClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to create accounts client: %q", err)
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
	client, err := armnetwork.NewPrivateEndpointsClient(
		c.subscriptionID,
		c.creds,
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
	client, err := armnetwork.NewPrivateEndpointsClient(
		c.subscriptionID,
		c.creds,
		&arm.ClientOptions{
			ClientOptions: *c.clientOpts,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get private endpoints client: %q", err)
	}

	privateLinkResourceID := formatPrivateLinkResourceID(
		c.subscriptionID,
		opts.ClusterResourceGroupName,
		opts.StorageAccountName,
	)
	subnetID := formatSubnetID(
		opts.SubnetName,
		opts.VNetName,
		opts.NetworkResourceGroupName,
		c.subscriptionID,
	)

	privateEndpointName := opts.PrivateEndpointName

	params := armnetwork.PrivateEndpoint{
		Location: to.StringPtr(opts.Location),
		Tags:     c.tagset,
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
	client, err := armnetwork.NewPrivateEndpointsClient(
		c.subscriptionID,
		c.creds,
		&arm.ClientOptions{
			ClientOptions: *c.clientOpts,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to get private endpoints client: %q", err)
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
	client, err := armprivatedns.NewPrivateZonesClient(c.subscriptionID, c.creds, &arm.ClientOptions{
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
			Tags:     c.tagset,
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
	client, err := armprivatedns.NewRecordSetsClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to get record sets client: %s", err)
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
	client, err := armprivatedns.NewRecordSetsClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to get record sets client: %s", err)
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
	client, err := armnetwork.NewPrivateDNSZoneGroupsClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to get private dns zone groups client: %q", err)
	}
	privateZoneID := formatPrivateDNSZoneID(c.subscriptionID, resourceGroupName, privateZoneName)
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
	client, err := armnetwork.NewPrivateDNSZoneGroupsClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to get private dns zone groups client: %q", err)
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
	client, err := armprivatedns.NewVirtualNetworkLinksClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to get virtual network links client: %s", err)
	}

	vnetID := formatVNetID(c.subscriptionID, networkResourceGroupName, vnetName)

	pollersResp, err := client.BeginCreateOrUpdate(
		ctx,
		clusterResourceGroupName,
		privateZoneName,
		linkName,
		armprivatedns.VirtualNetworkLink{
			Location: to.StringPtr(privateZoneLocation),
			Tags:     c.tagset,
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
	client, err := armprivatedns.NewVirtualNetworkLinksClient(c.subscriptionID, c.creds, &arm.ClientOptions{
		ClientOptions: *c.clientOpts,
	})
	if err != nil {
		return fmt.Errorf("failed to get virtual network links client: %s", err)
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
	client, err := armnetwork.NewInterfacesClient(c.subscriptionID, c.creds, &arm.ClientOptions{
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
	if opts.TenantID == "" {
		missingOpts = append(missingOpts, "'TenantID'")
	}
	if opts.ClientID == "" {
		missingOpts = append(missingOpts, "'ClientID'")
	}
	if opts.ClientSecret == "" && opts.FederatedTokenFile == "" && opts.Creds == nil {
		missingOpts = append(
			missingOpts,
			[]string{"'ClientSecret'", "'FederatedTokenFile'", "'Creds'"}...,
		)
	}
	if opts.SubscriptionID == "" {
		missingOpts = append(missingOpts, "'SubscriptionID'")
	}
	if len(missingOpts) > 0 {
		missing := strings.Join(missingOpts, ", ")
		return fmt.Errorf("client misconfigured, missing %s option(s)", missing)
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

	client, err := azblob.NewClient(blobURL, c.creds, &azblob.ClientOptions{
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
