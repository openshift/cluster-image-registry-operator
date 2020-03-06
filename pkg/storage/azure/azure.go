package azure

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/storage/mgmt/2019-04-01/storage"
	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/rest"
	"k8s.io/klog"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

const (
	blobFormatString = `https://%s.blob.core.windows.net`

	storageExistsReasonNotConfigured     = "StorageNotConfigured"
	storageExistsReasonConfigError       = "ConfigError"
	storageExistsReasonUserManaged       = "UserManaged"
	storageExistsReasonAzureError        = "AzureError"
	storageExistsReasonContainerNotFound = "ContainerNotFound"
	storageExistsReasonContainerExists   = "ContainerExists"
	storageExistsReasonContainerDeleted  = "ContainerDeleted"
	storageExistsReasonAccountDeleted    = "AccountDeleted"
)

var (
	// storageAccountInvalidCharRe is a regular expression for characters that
	// cannot be used in Azure storage accounts names (i.e. that are not
	// numbers nor lower-case letters) and that are not upper-case letters. If
	// you use this regular expression to filter invalid characters, you also
	// need to strings.ToLower to get a valid storage account name or an empty
	// string.
	storageAccountInvalidCharRe = regexp.MustCompile(`[^0-9A-Za-z]`)
)

type Azure struct {
	// IPI
	SubscriptionID string
	ClientID       string
	ClientSecret   string
	TenantID       string
	ResourceGroup  string
	Region         string

	// UPI
	AccountKey string
}

type errDoesNotExist struct {
	Err error
}

func (e *errDoesNotExist) Error() string {
	return e.Err.Error()
}

func getAzureConfigFromCloudSecret(creds *corev1.Secret) (*Azure, error) {
	cfg := &Azure{}
	cfg.SubscriptionID = string(creds.Data["azure_subscription_id"])
	cfg.ClientID = string(creds.Data["azure_client_id"])
	cfg.ClientSecret = string(creds.Data["azure_client_secret"])
	cfg.TenantID = string(creds.Data["azure_tenant_id"])
	cfg.ResourceGroup = string(creds.Data["azure_resourcegroup"])
	cfg.Region = string(creds.Data["azure_region"])
	return cfg, nil
}

func getAzureConfigFromUserSecret(sec *corev1.Secret) (*Azure, error) {
	cfg := &Azure{}
	var err error

	cfg.AccountKey, err = util.GetValueFromSecret(sec, "REGISTRY_STORAGE_AZURE_ACCOUNTKEY")
	if err != nil {
		return nil, err
	}
	if cfg.AccountKey == "" {
		return nil, fmt.Errorf("the secret %s/%s has an empty value for REGISTRY_STORAGE_AZURE_ACCOUNTKEY; the secret should be removed so that the operator can use cluster-wide secrets or it should contain a valid storage account access key", sec.Namespace, sec.Name)
	}

	return cfg, nil
}

// GetConfig reads configuration for the Azure cloud platform services.
func GetConfig(listers *regopclient.Listers) (*Azure, error) {
	sec, err := listers.Secrets.Get(defaults.ImageRegistryPrivateConfigurationUser)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("unable to get user provided secrets: %s", err)
		}

		creds, err := listers.Secrets.Get(defaults.CloudCredentialsName)
		if err != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials: %s", err)
		}
		return getAzureConfigFromCloudSecret(creds)
	}
	return getAzureConfigFromUserSecret(sec)
}

// generateAccountName returns a name that can be used for an Azure Storage
// Account. Storage account names must be between 3 and 24 characters in
// length and use numbers and lower-case letters only.
func generateAccountName(infrastructureName string) string {
	prefix := storageAccountInvalidCharRe.ReplaceAllString(infrastructureName, "")
	if prefix == "" {
		prefix = "imageregistry"
	}
	if len(prefix) > 24-5 {
		prefix = prefix[:24-5]
	}
	prefix = prefix + rand.String(5)
	return strings.ToLower(prefix)
}

func (d *driver) accountExists(storageAccountsClient storage.AccountsClient, accountName string) (storage.CheckNameAvailabilityResult, error) {
	return storageAccountsClient.CheckNameAvailability(
		d.Context,
		storage.AccountCheckNameAvailabilityParameters{
			Name: to.StringPtr(accountName),
			Type: to.StringPtr("Microsoft.Storage/storageAccounts"),
		})
}

func (d *driver) createStorageAccount(storageAccountsClient storage.AccountsClient, resourceGroupName, accountName, location string) error {
	klog.Infof("attempt to create azure storage account %s (resourceGroup=%q, location=%q)...", accountName, resourceGroupName, location)

	future, err := storageAccountsClient.Create(
		d.Context,
		resourceGroupName,
		accountName,
		storage.AccountCreateParameters{
			Kind:     storage.Storage,
			Location: to.StringPtr(location),
			Sku: &storage.Sku{
				Name: storage.StandardLRS,
			},
			AccountPropertiesCreateParameters: &storage.AccountPropertiesCreateParameters{},
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
	keysResponse, err := storageAccountsClient.ListKeys(d.Context, resourceGroupName, accountName)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to get keys for the storage account %s: %s", accountName, err)
		if e, ok := err.(autorest.DetailedError); ok {
			if e.StatusCode == http.StatusNotFound {
				return "", &errDoesNotExist{Err: wrappedErr}
			}
		}
		return "", wrappedErr
	}

	return *(*keysResponse.Keys)[0].Value, nil
}

func getStorageContainer(accountName, key, containerName string) (azblob.ContainerURL, error) {
	c, err := azblob.NewSharedKeyCredential(accountName, key)
	if err != nil {
		return azblob.ContainerURL{}, err
	}

	p := azblob.NewPipeline(c, azblob.PipelineOptions{
		Telemetry: azblob.TelemetryOptions{Value: parameters.UserAgent},
	})

	u, err := url.Parse(fmt.Sprintf(blobFormatString, accountName))
	if err != nil {
		return azblob.ContainerURL{}, err
	}

	service := azblob.NewServiceURL(*u, p)
	return service.NewContainerURL(containerName), nil
}

func (d *driver) createStorageContainer(accountName, key, containerName string) error {
	container, err := getStorageContainer(accountName, key, containerName)
	if err != nil {
		return err
	}

	_, err = container.Create(d.Context, azblob.Metadata{}, azblob.PublicAccessNone)
	return err
}

func (d *driver) deleteStorageContainer(accountName, key, containerName string) error {
	container, err := getStorageContainer(accountName, key, containerName)
	if err != nil {
		return err
	}

	_, err = container.Delete(d.Context, azblob.ContainerAccessConditions{})
	return err
}

type driver struct {
	Context    context.Context
	Config     *imageregistryv1.ImageRegistryConfigStorageAzure
	KubeConfig *rest.Config
	Listers    *regopclient.Listers
}

// NewDriver creates a new storage driver for Azure Blob Storage.
func NewDriver(ctx context.Context, c *imageregistryv1.ImageRegistryConfigStorageAzure, kubeconfig *rest.Config, listers *regopclient.Listers) *driver {
	return &driver{
		Context:    ctx,
		Config:     c,
		KubeConfig: kubeconfig,
		Listers:    listers,
	}
}

func (d *driver) storageAccountsClient(cfg *Azure) (storage.AccountsClient, error) {
	auth, err := auth.NewClientCredentialsConfig(cfg.ClientID, cfg.ClientSecret, cfg.TenantID).Authorizer()
	if err != nil {
		return storage.AccountsClient{}, err
	}

	storageAccountsClient := storage.NewAccountsClient(cfg.SubscriptionID)
	storageAccountsClient.Authorizer = auth
	storageAccountsClient.AddToUserAgent(parameters.UserAgent)

	return storageAccountsClient, nil
}

// ConfigEnv configures the environment variables that will be used in the
// image registry deployment.
func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: "azure"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_AZURE_CONTAINER", Value: d.Config.Container},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_AZURE_ACCOUNTNAME", Value: d.Config.AccountName},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_AZURE_ACCOUNTKEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: defaults.ImageRegistryPrivateConfiguration,
					},
					Key: "REGISTRY_STORAGE_AZURE_ACCOUNTKEY",
				},
			},
		},
	)
	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	return nil, nil, nil
}

// Secrets returns a map of the storage access secrets.
func (d *driver) Secrets() (map[string]string, error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	key := cfg.AccountKey
	if key == "" {
		storageAccountsClient, err := d.storageAccountsClient(cfg)
		if err != nil {
			return nil, err
		}

		key, err = d.getAccountPrimaryKey(storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName)
		if err != nil {
			return nil, err
		}
	}

	return map[string]string{
		"REGISTRY_STORAGE_AZURE_ACCOUNTKEY": key,
	}, nil
}

// containerExists determines whether or not an azure container exists
func (d *driver) containerExists(containerName string) (bool, error) {

	if d.Config.AccountName == "" || d.Config.Container == "" {
		return false, nil
	}

	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return false, err
	}

	key := cfg.AccountKey
	if key == "" {
		storageAccountsClient, err := d.storageAccountsClient(cfg)
		if err != nil {
			return false, err
		}

		// TODO: get key from the generated secret?
		key, err = d.getAccountPrimaryKey(storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName)
		if err != nil {
			return false, err
		}
	}

	c, err := azblob.NewSharedKeyCredential(d.Config.AccountName, key)
	if err != nil {
		return false, err
	}

	u, err := url.Parse(fmt.Sprintf(blobFormatString, d.Config.AccountName))
	if err != nil {
		return false, err
	}

	p := azblob.NewPipeline(c, azblob.PipelineOptions{
		Telemetry: azblob.TelemetryOptions{Value: parameters.UserAgent},
	})

	service := azblob.NewServiceURL(*u, p)
	container := service.NewContainerURL(d.Config.Container)
	_, err = container.GetProperties(d.Context, azblob.LeaseAccessConditions{})
	if e, ok := err.(azblob.StorageError); ok {
		if e.ServiceCode() == azblob.ServiceCodeContainerNotFound {
			return false, nil
		}
	}
	if err != nil {
		return false, fmt.Errorf("unable to get the storage container %s: %s", d.Config.Container, err)
	}

	return true, nil
}

// StorageExists checks if the storage container exists and is accessible.
func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	if d.Config.AccountName == "" || d.Config.Container == "" {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonNotConfigured, "Storage is not configured")
		return false, nil
	}

	exists, err := d.containerExists(d.Config.Container)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonAzureError, fmt.Sprintf("%s", err))
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
	if !reflect.DeepEqual(cr.Status.Storage.Azure, cr.Spec.Storage.Azure) {
		return true
	}
	return false
}

// CreateStorage attempts to create a storage account and a storage container.
func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonConfigError, fmt.Sprintf("Unable to get configuration: %s", err))
		return err
	}
	infra, err := d.Listers.Infrastructures.Get("cluster")
	if err != nil {
		return err
	}
	key := cfg.AccountKey
	if key != "" {
		// UPI
		if d.Config.AccountName == "" {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonNotConfigured, "Storage account key is provided, but account name is not specified")
			return nil
		}

		if d.Config.Container == "" {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonNotConfigured, "Storage account is provided, but container is not specified")
			return nil
		}

		cr.Status.StorageManaged = false
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			Azure: d.Config.DeepCopy(),
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionTrue, storageExistsReasonUserManaged, "Storage is managed by the user")
	} else {
		// IPI
		storageAccountsClient, err := d.storageAccountsClient(cfg)
		if err != nil {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to get accounts client: %s", err))
			return err
		}

		if d.Config.AccountName == "" {
			const maxAttempts = 10
			var lastErr error
			for i := 0; i < maxAttempts; i++ {
				accountName := generateAccountName(infra.Status.InfrastructureName)
				result, err := d.accountExists(storageAccountsClient, accountName)
				if err != nil {
					return err
				}
				if *result.NameAvailable {
					if err := d.createStorageAccount(storageAccountsClient, cfg.ResourceGroup, accountName, cfg.Region); err != nil {
						return err
					}
					d.Config.AccountName = accountName
					cr.Status.StorageManaged = true
					cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
						Azure: d.Config.DeepCopy(),
					}
					cr.Spec.Storage.Azure = d.Config.DeepCopy()
					break
				}
			}
			if d.Config.AccountName == "" {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonAzureError, fmt.Sprintf("Unable to create storage account: %s", lastErr))
				return fmt.Errorf("attmpts to create storage account failed, last error: %s", lastErr)
			}
		} else {
			// TODO: do we need to create a storage account if we are provided with its name?
			result, err := d.accountExists(storageAccountsClient, d.Config.AccountName)
			if err != nil {
				return err
			}
			if *result.NameAvailable {
				if err = d.createStorageAccount(storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName, cfg.Region); err != nil {
					util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to create storage account: %s", err))
					return err
				}
				cr.Status.StorageManaged = true
				cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
					Azure: d.Config.DeepCopy(),
				}
			}
		}

		var containerExists bool
		if len(d.Config.Container) != 0 {
			if containerExists, err = d.containerExists(d.Config.Container); err != nil {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonAzureError, fmt.Sprintf("%s", err))
			}
		}

		if len(d.Config.Container) != 0 && containerExists {
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				Azure: d.Config.DeepCopy(),
			}
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionTrue, storageExistsReasonContainerExists, "Azure container exists")
			return nil
		}

		var generatedName bool
		const numRetries = 5000
		key, err := d.getAccountPrimaryKey(storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName)
		if err != nil {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonAzureError, fmt.Sprintf("Unable to get account primary key: %s", err))
			return err
		}
		for i := 0; i < numRetries; i++ {
			// If the bucket name is blank, let's generate one
			if len(d.Config.Container) == 0 {
				// Container name must be between 3 and 63 characters long
				if d.Config.Container, err = util.GenerateStorageName(d.Listers, ""); err != nil {
					return err
				}
				generatedName = true
			}

			err = d.createStorageContainer(d.Config.AccountName, key, d.Config.Container)
			if err != nil {
				if e, ok := err.(azblob.StorageError); ok {
					switch e.ServiceCode() {
					case azblob.ServiceCodeContainerAlreadyExists:
						if len(d.Config.Container) != 0 && !generatedName {
							util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, "StoragePermissionDenied", "The container exists but we do not have permission to access it")
							break
						}
						d.Config.Container = ""
						continue
					default:
						util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonAzureError, fmt.Sprintf("%s", err))
						return err
					}
				} else {
					util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, string(e.ServiceCode()), fmt.Sprintf("Unable to create storage container: %s", err))
					return err
				}
			}
			cr.Status.StorageManaged = true
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				Azure: d.Config.DeepCopy(),
			}
			cr.Spec.Storage.Azure = d.Config.DeepCopy()
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionTrue, storageExistsReasonContainerExists, "Storage container exists")

			break
		}
	}
	return nil
}

// RemoveStorage deletes the storage medium that was created.
func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (retry bool, err error) {
	if cr.Status.StorageManaged != true {
		return false, nil
	}
	if d.Config.AccountName == "" {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonNotConfigured, "Storage is not configured")
		return false, nil
	}

	cfg, err := GetConfig(d.Listers)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonConfigError, fmt.Sprintf("Unable to get configuration: %s", err))
		return false, err
	}

	storageAccountsClient, err := d.storageAccountsClient(cfg)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to get accounts client: %s", err))
		return false, err
	}

	if d.Config.Container != "" {
		key, err := d.getAccountPrimaryKey(storageAccountsClient, cfg.ResourceGroup, d.Config.AccountName)
		if _, ok := err.(*errDoesNotExist); ok {
			d.Config.AccountName = ""
			cr.Spec.Storage.Azure.AccountName = "" // TODO
			cr.Status.Storage.Azure.AccountName = ""
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonContainerNotFound, fmt.Sprintf("Container has been already deleted: %s", err))
			return false, nil
		}
		if err != nil {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to get account primary keys: %s", err))
			return false, err
		}

		err = d.deleteStorageContainer(d.Config.AccountName, key, d.Config.Container)
		if err != nil {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionUnknown, storageExistsReasonAzureError, fmt.Sprintf("Unable to delete storage container: %s", err))
			return false, err // TODO: is it retryable?
		}

		d.Config.Container = ""
		cr.Spec.Storage.Azure.Container = "" // TODO: what if it was provided by a user?
		cr.Status.Storage.Azure.Container = ""
		util.UpdateCondition(cr, defaults.StorageExists, operatorapiv1.ConditionFalse, storageExistsReasonContainerDeleted, "Storage container has been deleted")
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
