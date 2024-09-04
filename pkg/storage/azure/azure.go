package azure

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/Azure/azure-pipeline-go/pipeline"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/services/storage/mgmt/2019-06-01/storage"
	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/Azure/go-autorest/autorest"
	autorestazure "github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/jongio/azidext/go/azidext"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	kcorelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/azure/azureclient"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

const (
	storageExistsReasonNotConfigured     = "StorageNotConfigured"
	storageExistsReasonConfigError       = "ConfigError"
	storageExistsReasonUserManaged       = "UserManaged"
	storageExistsReasonAzureError        = "AzureError"
	storageExistsReasonContainerNotFound = "ContainerNotFound"
	storageExistsReasonContainerExists   = "ContainerExists"
	storageExistsReasonContainerDeleted  = "ContainerDeleted"
	storageExistsReasonAccountDeleted    = "AccountDeleted"
	storageExistsReasonAccountNotFound   = "AccountNotFound"
)

// storageAccountInvalidCharRe is a regular expression for characters that
// cannot be used in Azure storage accounts names (i.e. that are not
// numbers nor lower-case letters) and that are not upper-case letters. If
// you use this regular expression to filter invalid characters, you also
// need to strings.ToLower to get a valid storage account name or an empty
// string.
var storageAccountInvalidCharRe = regexp.MustCompile(`[^0-9A-Za-z]`)

// Azure holds configuration used to reach Azure's endpoints.
type Azure struct {
	// IPI
	SubscriptionID     string
	ClientID           string
	ClientSecret       string
	TenantID           string
	ResourceGroup      string
	Region             string
	FederatedTokenFile string

	// UPI
	AccountKey string
}

type errDoesNotExist struct {
	Err error
}

func (e *errDoesNotExist) Error() string {
	return e.Err.Error()
}

// GetConfig reads configuration for the Azure cloud platform services. It first attempts to
// load credentials from ImageRegistryPrivateConfigurationUser secret, if this secret is not
// present this function loads credentials from cluster wide config present on secret
// CloudCredentialsName.
func GetConfig(secLister kcorelisters.SecretNamespaceLister, infraLister configlisters.InfrastructureLister) (*Azure, error) {
	sec, err := secLister.Get(defaults.ImageRegistryPrivateConfigurationUser)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("unable to get user provided secrets: %s", err)
		}

		// loads cluster wide configuration.
		if sec, err = secLister.Get(defaults.CloudCredentialsName); err != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials: %s", err)
		}

		cfg := &Azure{
			SubscriptionID:     string(sec.Data["azure_subscription_id"]),
			ClientID:           string(sec.Data["azure_client_id"]),
			ClientSecret:       string(sec.Data["azure_client_secret"]),
			TenantID:           string(sec.Data["azure_tenant_id"]),
			ResourceGroup:      string(sec.Data["azure_resourcegroup"]),
			Region:             string(sec.Data["azure_region"]),
			FederatedTokenFile: string(sec.Data["azure_federated_token_file"]),
		}

		// when using azure workload identities, the secret does not contain
		// a resource group, as it is not known at the time of its creation.
		if cfg.ResourceGroup == "" {
			infra, err := util.GetInfrastructure(infraLister)
			if err != nil {
				return nil, fmt.Errorf("unable to get infrastructure object: %s", err)
			}
			cfg.ResourceGroup = infra.Status.PlatformStatus.Azure.ResourceGroupName
		}

		return cfg, nil
	}

	// loads user provided account key.
	key, err := util.GetValueFromSecret(sec, "REGISTRY_STORAGE_AZURE_ACCOUNTKEY")
	if err != nil {
		return nil, err
	} else if key == "" {
		return nil, fmt.Errorf("the secret %s/%s has an empty value for "+
			"REGISTRY_STORAGE_AZURE_ACCOUNTKEY; the secret should be removed so that "+
			"the operator can use cluster-wide secrets or it should contain a valid "+
			"storage account access key", sec.Namespace, sec.Name,
		)
	}

	return &Azure{
		AccountKey: key,
	}, nil
}

func isAzureStackCloud(name string) bool {
	return strings.EqualFold(name, "AZURESTACKCLOUD")
}

func getEnvironmentByName(name string) (autorestazure.Environment, error) {
	if name == "" {
		return autorestazure.PublicCloud, nil
	}
	return autorestazure.EnvironmentFromName(name)
}

// generateAccountName returns a name that can be used for an Azure Storage
// Account. Storage account names must be between 3 and 24 characters in
// length and use numbers and lower-case letters only.
func generateAccountName(infrastructureName string) string {
	prefix := "imageregistry" + storageAccountInvalidCharRe.ReplaceAllString(infrastructureName, "")
	if len(prefix) > 24-5 {
		prefix = prefix[:24-5]
	}
	prefix = prefix + rand.String(5)
	return strings.ToLower(prefix)
}

func getBlobServiceURL(environment autorestazure.Environment, accountName string) (*url.URL, error) {
	return url.Parse("https://" + accountName + ".blob." + environment.StorageEndpointSuffix)
}

func (d *driver) accountExists(storageAccountsClient storage.AccountsClient, accountName string) (storage.CheckNameAvailabilityResult, error) {
	return storageAccountsClient.CheckNameAvailability(
		d.Context,
		storage.AccountCheckNameAvailabilityParameters{
			Name: to.StringPtr(accountName),
			Type: to.StringPtr("Microsoft.Storage/storageAccounts"),
		},
	)
}

func (d *driver) createStorageAccount(storageAccountsClient storage.AccountsClient, resourceGroupName, accountName, location, cloudName string, tagset map[string]*string) error {
	klog.Infof("attempt to create azure storage account %s (resourceGroup=%q, location=%q)...", accountName, resourceGroupName, location)

	kind := storage.StorageV2
	// NOTE: looks like this legacy version of the storage library does not support
	// disabling public network access at all... either we update the storage
	// account after creation using the new sdk, or we use the new sdk to create it
	// outside of azure stack hub, and the old sdk for azure stack hub.
	// Also doesn't support AccessKey usage disabling
	params := &storage.AccountPropertiesCreateParameters{
		EnableHTTPSTrafficOnly: to.BoolPtr(true),
		AllowBlobPublicAccess:  to.BoolPtr(false),
		MinimumTLSVersion:      storage.TLS12,
	}

	if isAzureStackCloud(cloudName) {
		// It seems Azure Stack Hub does not support new API.
		kind = storage.Storage
		params = &storage.AccountPropertiesCreateParameters{}
	}

	future, err := storageAccountsClient.Create(
		d.Context,
		resourceGroupName,
		accountName,
		storage.AccountCreateParameters{
			Kind:     kind,
			Location: to.StringPtr(location),
			Sku: &storage.Sku{
				Name: storage.StandardLRS,
			},
			AccountPropertiesCreateParameters: params,
			Tags:                              tagset,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to start creating storage account: %s", err)
	}

	// TODO: this may take up to 10 minutes
	err = future.WaitForCompletionRef(d.Context, storageAccountsClient.Client)
	if err != nil {
		return fmt.Errorf("failed to finish creating storage account: %s", err)
	}

	_, err = future.Result(storageAccountsClient)
	if err != nil {
		return fmt.Errorf("failed to create storage account: %s", err)
	}

	klog.Infof("azure storage account %s has been created", accountName)

	return nil
}

func (d *driver) getAccountPrimaryKey(storageAccountsClient storage.AccountsClient, resourceGroupName, accountName string) (string, error) {
	key, err := primaryKey.get(d.Context, storageAccountsClient, resourceGroupName, accountName)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to get keys for the storage account %s: %s", accountName, err)
		if e, ok := err.(autorest.DetailedError); ok {
			if e.StatusCode == http.StatusNotFound {
				return "", &errDoesNotExist{Err: wrappedErr}
			}
		}
		return "", wrappedErr
	}

	return key, nil
}

func (d *driver) getStorageContainer(environment autorestazure.Environment, accountName, key, containerName string) (azblob.ContainerURL, error) {
	c, err := azblob.NewSharedKeyCredential(accountName, key)
	if err != nil {
		return azblob.ContainerURL{}, err
	}

	p := azblob.NewPipeline(c, azblob.PipelineOptions{
		Telemetry:  azblob.TelemetryOptions{Value: defaults.UserAgent},
		HTTPSender: d.httpSender,
	})

	u, err := getBlobServiceURL(environment, accountName)
	if err != nil {
		return azblob.ContainerURL{}, err
	}

	service := azblob.NewServiceURL(*u, p)
	return service.NewContainerURL(containerName), nil
}

func (d *driver) createStorageContainer(environment autorestazure.Environment, accountName, key, containerName string) error {
	container, err := d.getStorageContainer(environment, accountName, key, containerName)
	if err != nil {
		return err
	}

	_, err = container.Create(d.Context, azblob.Metadata{}, azblob.PublicAccessNone)
	return err
}

func (d *driver) deleteStorageContainer(environment autorestazure.Environment, accountName, key, containerName string) error {
	container, err := d.getStorageContainer(environment, accountName, key, containerName)
	if err != nil {
		return err
	}

	_, err = container.Delete(d.Context, azblob.ContainerAccessConditions{})
	return err
}

type driver struct {
	// Context holds the operator's context that was passed to NewDriver.
	Context context.Context

	// Config is a subset of the image registry config. It may contain config
	// from spec or status depending on the caller intention.
	Config *imageregistryv1.ImageRegistryConfigStorageAzure

	// Listers is a collection of listers that the driver can use to obtain
	// additional objects from the cluster.
	Listers *regopclient.StorageListers

	// authorizer is for Azure autorest generated clients.
	// Added as a member to the struct to allow injection for testing.
	authorizer autorest.Authorizer

	// sender is for Azure autorest generated clients.
	// Added as a member to the struct to allow injection for testing.
	sender autorest.Sender

	// httpSender is for Azure Pipeline.
	// Added as a member to the struct to allow injection for testing.
	httpSender pipeline.Factory

	// policies is for new Azure Client Pipeline execution.
	// Added as a member to the struct to allow injection for testing.
	policies []policy.Policy
}

// NewDriver creates a new storage driver for Azure Blob Storage.
func NewDriver(ctx context.Context, c *imageregistryv1.ImageRegistryConfigStorageAzure, listers *regopclient.StorageListers) *driver {
	return &driver{
		Context: ctx,
		Config:  c,
		Listers: listers,
	}
}

func (d *driver) newAzClient(cfg *Azure, environment autorestazure.Environment, tagset map[string]*string) (*azureclient.Client, error) {
	client, err := azureclient.New(&azureclient.Options{
		Environment:        environment,
		TenantID:           cfg.TenantID,
		ClientID:           cfg.ClientID,
		ClientSecret:       cfg.ClientSecret,
		FederatedTokenFile: cfg.FederatedTokenFile,
		SubscriptionID:     cfg.SubscriptionID,
		TagSet:             tagset,
		Policies:           d.policies,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (d *driver) storageAccountsClient(cfg *Azure, environment autorestazure.Environment) (storage.AccountsClient, error) {
	storageAccountsClient := storage.NewAccountsClientWithBaseURI(environment.ResourceManagerEndpoint, cfg.SubscriptionID)
	storageAccountsClient.PollingDelay = 10 * time.Second
	storageAccountsClient.PollingDuration = 3 * time.Minute
	storageAccountsClient.RetryAttempts = 1
	_ = storageAccountsClient.AddToUserAgent(defaults.UserAgent)

	if d.authorizer != nil && d.sender != nil {
		storageAccountsClient.Authorizer = d.authorizer
		storageAccountsClient.Sender = d.sender
		return storageAccountsClient, nil
	}

	cloudConfig := cloud.Configuration{
		ActiveDirectoryAuthorityHost: environment.ActiveDirectoryEndpoint,
		Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
			cloud.ResourceManager: {
				Audience: environment.TokenAudience,
				Endpoint: environment.ResourceManagerEndpoint,
			},
		},
	}

	var (
		cred azcore.TokenCredential
		err  error
	)
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		options := azidentity.WorkloadIdentityCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				Cloud: cloudConfig,
			},
			ClientID:      cfg.ClientID,
			TenantID:      cfg.TenantID,
			TokenFilePath: cfg.FederatedTokenFile,
		}
		cred, err = azidentity.NewWorkloadIdentityCredential(&options)
		if err != nil {
			return storage.AccountsClient{}, err
		}
	} else {
		options := azidentity.ClientSecretCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				Cloud: cloudConfig,
			},
		}
		cred, err = azidentity.NewClientSecretCredential(cfg.TenantID, cfg.ClientID, cfg.ClientSecret, &options)
		if err != nil {
			return storage.AccountsClient{}, err
		}
	}

	scope := environment.TokenAudience
	if !strings.HasSuffix(scope, "/.default") {
		scope += "/.default"
	}

	storageAccountsClient.Authorizer = azidext.NewTokenCredentialAdapter(cred, []string{scope})

	return storageAccountsClient, nil
}

func (d *driver) getKey(cfg *Azure, environment autorestazure.Environment) (string, error) {
	if cfg.AccountKey != "" {
		return cfg.AccountKey, nil
	}

	storageAccountsClient, err := d.storageAccountsClient(cfg, environment)
	if err != nil {
		return "", err
	}

	key, err := d.getAccountPrimaryKey(storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName)
	if err != nil {
		return "", err
	}

	return key, nil
}

func (d *driver) CABundle() (string, bool, error) {
	return "", true, nil
}

// ConfigEnv configures the environment variables that will be used in the
// image registry deployment.
func (d *driver) ConfigEnv() (envs envvar.List, err error) {
	cfg, err := GetConfig(d.Listers.Secrets, d.Listers.Infrastructures)
	if err != nil {
		return nil, err
	}

	environment, err := getEnvironmentByName(d.Config.CloudName)
	if err != nil {
		return nil, err
	}

	key := cfg.AccountKey
	federated_token := cfg.FederatedTokenFile
	if key == "" && federated_token == "" {
		storageAccountsClient, err := d.storageAccountsClient(cfg, environment)
		if err != nil {
			return nil, err
		}

		key, err = d.getAccountPrimaryKey(storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName)
		if err != nil {
			return nil, err
		}
	}

	if key != "" {
		envs = append(envs,
			envvar.EnvVar{Name: "REGISTRY_STORAGE_AZURE_ACCOUNTKEY", Value: key, Secret: true},
		)
	}

	// the AZURE_ vars used to configure workload identity are taken
	// from https://github.com/distribution/distribution/blob/6a57630cf40122000083e60bcb7e97c50a904c5e/vendor/github.com/Azure/azure-sdk-for-go/sdk/azidentity/default_azure_credential.go#LL86C43-L86C63
	if federated_token != "" {
		envs = append(envs,
			// NOTE: these env vars are not prepended with REGISTRY_STORAGE
			// because they're exported for the azure-sdk, not the registry.
			// we do this as a transparent way to support workload identity in the registry.
			envvar.EnvVar{Name: "AZURE_CLIENT_ID", Value: cfg.ClientID},
			envvar.EnvVar{Name: "AZURE_TENANT_ID", Value: cfg.TenantID},
			envvar.EnvVar{Name: "AZURE_FEDERATED_TOKEN_FILE", Value: federated_token},
			envvar.EnvVar{Name: "AZURE_AUTHORITY_HOST", Value: environment.ActiveDirectoryEndpoint},
		)
	}

	envs = append(envs,
		envvar.EnvVar{Name: "REGISTRY_STORAGE", Value: "azure"},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_AZURE_CONTAINER", Value: d.Config.Container},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_AZURE_ACCOUNTNAME", Value: d.Config.AccountName},
	)

	if d.Config.CloudName != "" {
		envs = append(envs, envvar.EnvVar{Name: "REGISTRY_STORAGE_AZURE_REALM", Value: environment.StorageEndpointSuffix})
	}

	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	return nil, nil, nil
}

func (d *driver) VolumeSecrets() (map[string]string, error) {
	return nil, nil
}

// containerExists determines whether or not an azure container exists
func (d *driver) containerExists(ctx context.Context, environment autorestazure.Environment, accountName, key, containerName string) (bool, error) {
	if accountName == "" || containerName == "" {
		return false, nil
	}

	c, err := azblob.NewSharedKeyCredential(accountName, key)
	if err != nil {
		return false, err
	}

	u, err := getBlobServiceURL(environment, accountName)
	if err != nil {
		return false, err
	}

	p := azblob.NewPipeline(c, azblob.PipelineOptions{
		Telemetry:  azblob.TelemetryOptions{Value: defaults.UserAgent},
		HTTPSender: d.httpSender,
	})

	service := azblob.NewServiceURL(*u, p)
	container := service.NewContainerURL(containerName)
	_, err = container.GetProperties(ctx, azblob.LeaseAccessConditions{})
	if e, ok := err.(azblob.StorageError); ok {
		if e.ServiceCode() == azblob.ServiceCodeContainerNotFound {
			return false, nil
		}
	}
	if err != nil {
		return false, fmt.Errorf("unable to get the storage container %s: %s", containerName, err)
	}

	return true, nil
}

func (d *driver) storageExistsViaTrack2SDK(cr *imageregistryv1.Config, cfg *Azure, environment autorestazure.Environment) (exists bool, err error) {
	key := cfg.AccountKey
	federated_token := cfg.FederatedTokenFile
	azClient, err := d.newAzClient(cfg, environment, nil)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to create azure client: %s", err))
		return false, err
	}
	if key == "" && federated_token == "" {
		key, err = d.getKey(cfg, environment)
		if err != nil {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to get storage account key: %s", err))
			return false, err
		}

	}

	u, err := getBlobServiceURL(environment, d.Config.AccountName)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonConfigError, fmt.Sprintf("Unable to parse blob url: %s", err))
		return false, err
	}
	blobClient, err := azClient.NewBlobClient(environment, d.Config.AccountName, key, fmt.Sprintf("%s://%s/", u.Scheme, u.Host))
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to create blob client: %s", err))
		return false, err
	}

	exists, err = blobClient.ContainerExists(d.Context, d.Config.AccountName, d.Config.Container)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("%s", err))
		return false, err
	}
	if !exists {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonContainerNotFound, fmt.Sprintf("Could not find storage container %s", d.Config.Container))
		return false, nil
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionTrue, storageExistsReasonContainerExists, "Storage container exists")
	return true, nil
}

// StorageExists checks if the storage container exists and is accessible.
func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	if d.Config.AccountName == "" || d.Config.Container == "" {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonNotConfigured, "Storage is not configured")
		return false, nil
	}

	cfg, err := GetConfig(d.Listers.Secrets, d.Listers.Infrastructures)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonConfigError, fmt.Sprintf("Unable to get configuration: %s", err))
		return false, err
	}

	environment, err := getEnvironmentByName(d.Config.CloudName)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonConfigError, fmt.Sprintf("Unable to get cloud environment: %s", err))
		return false, err
	}

	if !isAzureStackCloud(d.Config.CloudName) {
		return d.storageExistsViaTrack2SDK(cr, cfg, environment)
	}

	key, err := d.getKey(cfg, environment)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to get storage account key: %s", err))
		return false, err
	}

	exists, err := d.containerExists(d.Context, environment, d.Config.AccountName, key, d.Config.Container)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("%s", err))
		return false, err
	}
	if !exists {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonContainerNotFound, fmt.Sprintf("Could not find storage container %s", d.Config.Container))
		return false, nil
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionTrue, storageExistsReasonContainerExists, "Storage container exists")
	return true, nil
}

// StorageChanged checks if the storage configuration has changed.
func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	return !reflect.DeepEqual(cr.Status.Storage.Azure, cr.Spec.Storage.Azure)
}

func (d *driver) assurePrivateAccount(cfg *Azure, infra *configv1.Infrastructure, tagset map[string]*string, accountName string) (string, error) {
	if d.Config.NetworkAccess == nil || d.Config.NetworkAccess.Type == imageregistryv1.AzureNetworkAccessTypeExternal {
		// user did not request private storage account setup - skip.
		return "", nil
	}

	environment, err := getEnvironmentByName(d.Config.CloudName)
	if err != nil {
		return "", err
	}
	azclient, err := d.newAzClient(cfg, environment, tagset)
	if err != nil {
		return "", err
	}

	internalConfig := d.Config.NetworkAccess.Internal
	if internalConfig == nil {
		// avoid nil pointer checks everywhere - this will happen when the user
		// wants the operator to discover vnet and subnet names.
		internalConfig = &imageregistryv1.AzureNetworkAccessInternal{}
	}

	privateEndpointName := internalConfig.PrivateEndpointName
	if privateEndpointName == "" {
		privateEndpointName = generateAccountName(infra.Status.InfrastructureName)
	}

	networkResourceGroup := cfg.ResourceGroup
	if internalConfig.NetworkResourceGroupName != "" {
		networkResourceGroup = internalConfig.NetworkResourceGroupName
	}

	// the last step in this function is to disable public network for the
	// storage account - if we already did that, then none of the steps
	// below need to be executed.
	if azclient.IsStorageAccountPrivate(d.Context, cfg.ResourceGroup, accountName) {
		return privateEndpointName, nil
	}

	if internalConfig.VNetName == "" {
		tagKey := fmt.Sprintf("kubernetes.io_cluster.%s", infra.Status.InfrastructureName)
		vnet, err := azclient.GetVNetByTag(d.Context, networkResourceGroup, tagKey, "owned", "shared")
		if err != nil {
			return "", fmt.Errorf("failed to discover vnet name, please provide network details manually: %q", err)
		}
		internalConfig.VNetName = *vnet.Name
	}

	if internalConfig.SubnetName == "" {
		subnet, err := azclient.GetSubnetsByVNet(d.Context, networkResourceGroup, internalConfig.VNetName)
		if err != nil {
			return "", fmt.Errorf("failed to discover subnet name, please provide network details manually: %q", err)
		}
		internalConfig.SubnetName = *subnet.Name
	}

	klog.V(3).Infof("configuring private endpoint %q for storage account...", privateEndpointName)
	pe, err := azclient.CreatePrivateEndpoint(
		d.Context,
		&azureclient.PrivateEndpointCreateOptions{
			Location:                 cfg.Region,
			ClusterResourceGroupName: cfg.ResourceGroup,
			NetworkResourceGroupName: networkResourceGroup,
			VNetName:                 internalConfig.VNetName,
			SubnetName:               internalConfig.SubnetName,
			PrivateEndpointName:      privateEndpointName,
			StorageAccountName:       accountName,
		},
	)
	if err != nil {
		return "", err
	}
	klog.V(3).Info("private endpoint configured")

	klog.V(3).Info("configuring private DNS...")
	if err := azclient.ConfigurePrivateDNS(
		d.Context, pe, cfg.ResourceGroup, networkResourceGroup, internalConfig.VNetName, accountName,
	); err != nil {
		return privateEndpointName, err
	}
	klog.V(3).Info("private DNS configured")

	klog.V(3).Infof("disabling public network access for storage account %q...", accountName)
	if err := azclient.UpdateStorageAccountNetworkAccess(d.Context, cfg.ResourceGroup, accountName, false); err != nil {
		return privateEndpointName, err
	}

	klog.Infof(
		"storage account %q is now served by private endpoint %q. public network access is disabled",
		accountName, privateEndpointName,
	)

	d.Config.NetworkAccess.Internal = internalConfig

	return privateEndpointName, nil
}

// assureStorageAccount makes sure there is a storage account in place and apply any provided tags.
// If no storage account name is provided it attempts to generate one. Returns the account name
// (either the one provided or the one generated), if the account was created or was already there and an error.
func (d *driver) assureStorageAccount(cfg *Azure, infra *configv1.Infrastructure, tagset map[string]*string) (string, bool, error) {
	environment, err := getEnvironmentByName(d.Config.CloudName)
	if err != nil {
		return "", false, err
	}

	storageAccountsClient, err := d.storageAccountsClient(cfg, environment)
	if err != nil {
		return "", false, err
	}

	var accountNameGenerated bool
	accountName := d.Config.AccountName
	if accountName == "" {
		accountNameGenerated = true
		accountName = generateAccountName(infra.Status.InfrastructureName)
	}

	result, err := d.accountExists(storageAccountsClient, accountName)
	if err != nil {
		return "", false, err
	}

	// if the generated storage account is not available we return an error.
	if accountNameGenerated && !*result.NameAvailable {
		return "", false, fmt.Errorf("create storage account failed, name not available")
	}

	// regardless if the storage account name was provided by the user or we generated it,
	// if it is available, we do attempt to create it.
	var storageAccountCreated bool
	if *result.NameAvailable {
		storageAccountCreated = true
		if err := d.createStorageAccount(
			storageAccountsClient, cfg.ResourceGroup, accountName, cfg.Region, d.Config.CloudName, tagset,
		); err != nil {
			return "", false, err
		}
	}

	if !isAzureStackCloud(d.Config.CloudName) && cfg.FederatedTokenFile != "" {
		azClient, err := d.newAzClient(cfg, environment, tagset)
		if err != nil {
			return "", false, err
		}
		err = azClient.DisableStorageAccountAccessKeyAccess(d.Context, cfg.ResourceGroup, accountName)
		if err != nil {
			return "", false, err
		}
	}

	return accountName, storageAccountCreated, nil
}

func (d *driver) assureContainerViaTrack2SDK(cfg *Azure) (string, bool, error) {
	environment, err := getEnvironmentByName(d.Config.CloudName)
	if err != nil {
		return "", false, err
	}
	key := cfg.AccountKey
	federated_token := cfg.FederatedTokenFile
	azClient, err := d.newAzClient(cfg, environment, nil)
	if err != nil {
		return "", false, err
	}
	if key == "" && federated_token == "" {
		storageAccountsClient, err := d.storageAccountsClient(cfg, environment)
		if err != nil {
			return "", false, err
		}
		key, err = d.getAccountPrimaryKey(
			storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName,
		)
		if err != nil {
			return "", false, err
		}
	}

	u, err := getBlobServiceURL(environment, d.Config.AccountName)
	if err != nil {
		return "", false, err
	}
	blobClient, err := azClient.NewBlobClient(environment, d.Config.AccountName, key, fmt.Sprintf("%s://%s/", u.Scheme, u.Host))
	if err != nil {
		return "", false, err
	}
	if d.Config.Container == "" {
		containerName, err := util.GenerateStorageName(d.Listers, "")
		if err != nil {
			return "", false, err
		}

		if err = blobClient.CreateStorageContainer(
			d.Context, containerName,
		); err != nil {
			return "", false, err
		}

		return containerName, true, nil
	}

	if exists, err := blobClient.ContainerExists(
		d.Context, d.Config.AccountName, d.Config.Container,
	); err != nil {
		return "", false, err
	} else if exists {
		return d.Config.Container, false, nil
	}

	if err = blobClient.CreateStorageContainer(
		d.Context, d.Config.Container,
	); err != nil {
		return "", false, err
	}
	return d.Config.Container, true, nil
}

// assureContainer makes sure we have a container in place. Container name may be provided or
// generated automatically. Returns the container name (the provided one or the automatically
// generated), if the container was created or was already there and an error.
func (d *driver) assureContainer(cfg *Azure) (string, bool, error) {
	environment, err := getEnvironmentByName(d.Config.CloudName)
	if err != nil {
		return "", false, err
	}

	storageAccountsClient, err := d.storageAccountsClient(cfg, environment)
	if err != nil {
		return "", false, err
	}

	key, err := d.getAccountPrimaryKey(
		storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName,
	)
	if err != nil {
		return "", false, err
	}

	if d.Config.Container == "" {
		containerName, err := util.GenerateStorageName(d.Listers, "")
		if err != nil {
			return "", false, err
		}

		if err = d.createStorageContainer(
			environment, d.Config.AccountName, key, containerName,
		); err != nil {
			return "", false, err
		}

		return containerName, true, nil
	}

	if exists, err := d.containerExists(
		d.Context, environment, d.Config.AccountName, key, d.Config.Container,
	); err != nil {
		return "", false, err
	} else if exists {
		return d.Config.Container, false, nil
	}

	if err = d.createStorageContainer(
		environment, d.Config.AccountName, key, d.Config.Container,
	); err != nil {
		return "", false, err
	}
	return d.Config.Container, true, nil
}

// processUPI verifies if user provided configuration is complete and updates conditions
// and status appropriately.
func (d *driver) processUPI(cr *imageregistryv1.Config) {
	if d.Config.AccountName == "" {
		util.UpdateCondition(
			cr,
			defaults.StorageExists,
			operatorapiv1.ConditionFalse,
			storageExistsReasonNotConfigured,
			"Storage account key is provided, but account name is not specified",
		)
		return
	}

	if d.Config.Container == "" {
		util.UpdateCondition(
			cr,
			defaults.StorageExists,
			operatorapiv1.ConditionFalse,
			storageExistsReasonNotConfigured,
			"Storage account is provided, but container is not specified",
		)
		return
	}

	// We only set the storage management if it is not already set.
	if cr.Spec.Storage.ManagementState == "" {
		cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateUnmanaged
	}

	cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
		Azure: d.Config.DeepCopy(),
	}

	util.UpdateCondition(
		cr,
		defaults.StorageExists,
		operatorapiv1.ConditionTrue,
		storageExistsReasonUserManaged,
		"Storage is managed by the user",
	)
}

// CreateStorage attempts to create a storage account and a storage container.
func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	cfg, err := GetConfig(d.Listers.Secrets, d.Listers.Infrastructures)
	if err != nil {
		util.UpdateCondition(
			cr,
			defaults.StorageExists,
			operatorapiv1.ConditionUnknown,
			storageExistsReasonConfigError,
			fmt.Sprintf("Unable to get configuration: %s", err),
		)
		return err
	}

	// if AccountKey is present in our configuration it means it was provided by the user
	// so we only verify if everything we need is in place.
	if cfg.AccountKey != "" {
		d.processUPI(cr)
		return nil
	}

	infra, err := util.GetInfrastructure(d.Listers.Infrastructures)
	if err != nil {
		util.UpdateCondition(
			cr,
			defaults.StorageExists,
			operatorapiv1.ConditionUnknown,
			storageExistsReasonConfigError,
			fmt.Sprintf("Unable to get infrastructure: %s", err),
		)
		return err
	}

	if d.Config.CloudName == "" && d.Config.AccountName == "" {
		platformStatus := infra.Status.PlatformStatus
		if platformStatus != nil &&
			platformStatus.Type == configv1.AzurePlatformType &&
			platformStatus.Azure != nil {
			d.Config.CloudName = string(platformStatus.Azure.CloudName)
		}
	}

	// Tag the storage account with the openshiftClusterID
	// along with any user defined tags from the cluster configuration
	klog.V(2).Info("setting azure storage account tags")
	tagset := map[string]*string{
		fmt.Sprintf("kubernetes.io_cluster.%s", infra.Status.InfrastructureName): to.StringPtr("owned"),
	}

	// at this stage we are not keeping user tags in sync. as per enhancement proposal
	// we only set user provided tags when we created the bucket.
	hasAzureStatus := infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.Azure != nil && infra.Status.PlatformStatus.Azure.ResourceTags != nil
	if hasAzureStatus {
		klog.V(5).Infof("user has provided %d tags", len(infra.Status.PlatformStatus.Azure.ResourceTags))
		for _, tag := range infra.Status.PlatformStatus.Azure.ResourceTags {
			klog.V(5).Infof("user has provided storage account tag: %s: %s", tag.Key, tag.Value)
			tagset[tag.Key] = to.StringPtr(tag.Value)
		}
	}
	klog.V(5).Infof("tagging storage account with tags: %+v", tagset)

	storageAccountName, storageAccountCreated, err := d.assureStorageAccount(cfg, infra, tagset)
	if err != nil {
		util.UpdateCondition(
			cr,
			defaults.StorageExists,
			operatorapiv1.ConditionUnknown,
			storageExistsReasonAzureError,
			fmt.Sprintf("Unable to process storage account: %s", err),
		)
		return err
	}
	d.Config.AccountName = storageAccountName

	var containerName string
	var containerCreated bool
	if isAzureStackCloud(d.Config.CloudName) {
		containerName, containerCreated, err = d.assureContainer(cfg)
	} else {
		containerName, containerCreated, err = d.assureContainerViaTrack2SDK(cfg)
	}
	if err != nil {
		util.UpdateCondition(
			cr,
			defaults.StorageExists,
			operatorapiv1.ConditionUnknown,
			storageExistsReasonAzureError,
			fmt.Sprintf("Unable to process storage container: %s", err),
		)
		return err
	}
	d.Config.Container = containerName

	privateEndpointName, err := d.assurePrivateAccount(cfg, infra, tagset, storageAccountName)
	if err != nil {
		util.UpdateCondition(
			cr,
			defaults.StorageExists,
			operatorapiv1.ConditionUnknown,
			storageExistsReasonAzureError,
			fmt.Sprintf("Unable to process private endpoint: %s", err),
		)
		return err
	}
	if privateEndpointName != "" {
		// in most cases, a call to d.assurePrivateAccount will not result
		// in the creation of a private endpoint. this will only happen
		// when users explicitly request so by setting
		// VNetName and SubnetName
		// in the registry config. only then we set the private endpoint name.
		d.Config.NetworkAccess.Internal.PrivateEndpointName = privateEndpointName
	}

	// We only set the storage management if it is not already set.
	if cr.Spec.Storage.ManagementState == "" {
		if storageAccountCreated || containerCreated {
			cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateManaged
		} else {
			cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateUnmanaged
		}
	}

	cr.Spec.Storage.Azure = d.Config.DeepCopy()
	cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
		Azure: d.Config.DeepCopy(),
	}

	util.UpdateCondition(
		cr,
		defaults.StorageExists,
		operatorapiv1.ConditionTrue,
		storageExistsReasonContainerExists,
		"Storage container exists",
	)
	return nil
}

func (d *driver) removeStorageContainerViaTrack2SDK(cr *imageregistryv1.Config, cfg *Azure, environment autorestazure.Environment, storageAccountsClient storage.AccountsClient) (accountNotFound bool, err error) {
	key := cfg.AccountKey
	federated_token := cfg.FederatedTokenFile
	azClient, err := d.newAzClient(cfg, environment, nil)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to create azure client: %s", err))
		return false, err
	}
	if key == "" && federated_token == "" {
		key, err = d.getAccountPrimaryKey(storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName)
		if _, ok := err.(*errDoesNotExist); ok {
			d.Config.AccountName = ""
			cr.Spec.Storage.Azure.AccountName = "" // TODO
			cr.Status.Storage.Azure.AccountName = ""
			// TODO: The update condition should suggest Account doesn't exist rather than Container
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonContainerNotFound, fmt.Sprintf("Container has been already deleted: %s", err))
			return true, nil
		}
		if err != nil {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to get account primary keys: %s", err))
			return false, err
		}
	} else {
		result, err := d.accountExists(storageAccountsClient, d.Config.AccountName)
		if err != nil {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to check account existence: %s", err))
			return false, err
		}

		// if the storage account is not available we return no error.
		if *result.NameAvailable {
			d.Config.AccountName = ""
			cr.Spec.Storage.Azure.AccountName = ""
			cr.Status.Storage.Azure.AccountName = ""
			// TODO: The update condition should suggest Account doesn't exist rather than Container
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonContainerNotFound, fmt.Sprintf("Container has been already deleted: %s", err))
			return true, nil
		}
	}

	u, err := getBlobServiceURL(environment, d.Config.AccountName)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonConfigError, fmt.Sprintf("Unable to parse blob url: %s", err))
		return false, err
	}
	blobClient, err := azClient.NewBlobClient(environment, d.Config.AccountName, key, fmt.Sprintf("%s://%s/", u.Scheme, u.Host))
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to create blob client: %s", err))
		return false, err
	}
	err = blobClient.DeleteStorageContainer(d.Context, d.Config.Container)
	if err != nil {
		if bloberror.HasCode(err, bloberror.AuthorizationPermissionMismatch) {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to delete storage container due to delete container permission missing, trying account deletion: %s", err))
			return false, nil
		} else if bloberror.HasCode(err, "AccountNotFound") {
			d.Config.AccountName = ""
			cr.Spec.Storage.Azure.AccountName = ""
			cr.Status.Storage.Azure.AccountName = ""
			// TODO: The update condition should suggest Account doesn't exist rather than Container
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonContainerNotFound, fmt.Sprintf("Container has been already deleted: %s", err))
			return true, nil
		} else if !bloberror.HasCode(err, bloberror.ContainerNotFound) {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to delete storage container: %s", err))
			return false, err
		}
	}

	d.Config.Container = ""
	cr.Spec.Storage.Azure.Container = "" // TODO: what if it was provided by a user?
	cr.Status.Storage.Azure.Container = ""
	util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonContainerDeleted, "Storage container has been deleted")
	return false, nil
}

func (d *driver) removeStorageContainer(cr *imageregistryv1.Config, cfg *Azure, environment autorestazure.Environment, storageAccountsClient storage.AccountsClient) (accountNotFound bool, err error) {
	key, err := d.getAccountPrimaryKey(storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName)
	if _, ok := err.(*errDoesNotExist); ok {
		d.Config.AccountName = ""
		cr.Spec.Storage.Azure.AccountName = "" // TODO
		cr.Status.Storage.Azure.AccountName = ""
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonAccountNotFound, fmt.Sprintf("Account has been already deleted: %s", err))
		return true, nil
	}
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to get account primary keys: %s", err))
		return false, err
	}

	err = d.deleteStorageContainer(environment, d.Config.AccountName, key, d.Config.Container)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to delete storage container: %s", err))
		return false, err // TODO: is it retryable?
	}

	d.Config.Container = ""
	cr.Spec.Storage.Azure.Container = "" // TODO: what if it was provided by a user?
	cr.Status.Storage.Azure.Container = ""
	util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonContainerDeleted, "Storage container has been deleted")
	return false, nil
}

// RemoveStorage deletes the storage medium that was created.
func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (retry bool, err error) {
	if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged {
		return false, nil
	}
	if d.Config.AccountName == "" {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonNotConfigured, "Storage is not configured")
		return false, nil
	}

	cfg, err := GetConfig(d.Listers.Secrets, d.Listers.Infrastructures)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonConfigError, fmt.Sprintf("Unable to get configuration: %s", err))
		return false, err
	}

	environment, err := getEnvironmentByName(d.Config.CloudName)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonConfigError, fmt.Sprintf("Unable to get cloud environment: %s", err))
		return false, err
	}

	storageAccountsClient, err := d.storageAccountsClient(cfg, environment)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to get accounts client: %s", err))
		return false, err
	}

	if d.Config.NetworkAccess != nil && d.Config.NetworkAccess.Internal != nil && d.Config.NetworkAccess.Internal.PrivateEndpointName != "" {
		azclient, err := azureclient.New(&azureclient.Options{
			Environment:        environment,
			TenantID:           cfg.TenantID,
			ClientID:           cfg.ClientID,
			ClientSecret:       cfg.ClientSecret,
			FederatedTokenFile: cfg.FederatedTokenFile,
			SubscriptionID:     cfg.SubscriptionID,
		})
		if err != nil {
			util.UpdateCondition(
				cr,
				defaults.StorageExists,
				operatorapiv1.ConditionUnknown,
				storageExistsReasonAzureError,
				fmt.Sprintf("Unable to get azure client: %s", err),
			)
			return false, err
		}
		if err := azclient.DestroyPrivateDNS(
			d.Context,
			cfg.ResourceGroup,
			d.Config.NetworkAccess.Internal.PrivateEndpointName,
			d.Config.NetworkAccess.Internal.VNetName,
			d.Config.AccountName,
		); err != nil {
			util.UpdateCondition(
				cr,
				defaults.StorageExists,
				operatorapiv1.ConditionUnknown,
				storageExistsReasonAzureError,
				fmt.Sprintf("Unable to destroy private dns: %q", err),
			)
			return false, err
		}
		if err := azclient.DeletePrivateEndpoint(
			d.Context, cfg.ResourceGroup, d.Config.NetworkAccess.Internal.PrivateEndpointName,
		); err != nil {
			util.UpdateCondition(
				cr,
				defaults.StorageExists,
				operatorapiv1.ConditionUnknown,
				storageExistsReasonAzureError,
				fmt.Sprintf("Unable to delete private endpoint: %q", err),
			)
			return false, err
		}
		d.Config.NetworkAccess = nil
	}

	if d.Config.Container != "" {
		var accountNotFound bool
		if isAzureStackCloud(d.Config.CloudName) {
			accountNotFound, err = d.removeStorageContainer(cr, cfg, environment, storageAccountsClient)
		} else {
			accountNotFound, err = d.removeStorageContainerViaTrack2SDK(cr, cfg, environment, storageAccountsClient)
		}
		if err != nil {
			return false, err
		}
		if accountNotFound {
			return false, nil
		}
	}

	_, err = storageAccountsClient.Delete(d.Context, cfg.ResourceGroup, d.Config.AccountName)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonAzureError, fmt.Sprintf("Unable to delete storage account: %s", err))
		return false, err
	}

	d.Config.AccountName = ""
	cr.Spec.Storage.Azure.AccountName = "" // TODO
	cr.Status.Storage.Azure.AccountName = ""
	util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonAccountDeleted, "Storage account has been deleted")

	return false, nil
}

// ID return the underlying storage identificator, on this case the Azure
// container name.
func (d *driver) ID() string {
	return d.Config.Container
}
