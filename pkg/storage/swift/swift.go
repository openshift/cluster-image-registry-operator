package swift

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/objectstorage/v1/containers"
	"github.com/gophercloud/gophercloud/openstack/objectstorage/v1/objects"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/goware/urlx"
	yamlv2 "gopkg.in/yaml.v2"

	corev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	k8sutilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

type Swift struct {
	AuthURL                     string
	Username                    string
	Password                    string
	Tenant                      string
	TenantID                    string
	Domain                      string
	DomainID                    string
	RegionName                  string
	IdentityAPIVersion          string
	ApplicationCredentialID     string
	ApplicationCredentialName   string
	ApplicationCredentialSecret string
}

type driver struct {
	// Config is a struct where the basic configuration is stored
	Config *imageregistryv1.ImageRegistryConfigStorageSwift
	// Listers are used to download OpenStack credentials from the native secret
	Listers *regopclient.StorageListers
}

// replaceEmpty is a helper function to replace empty fields with another field
func replaceEmpty(a string, b string) string {
	if a == "" {
		return b
	}
	return a
}

// IsSwiftEnabled checks if Swift service is available for OpenStack platform
func IsSwiftEnabled(listers *regopclient.StorageListers) (bool, error) {
	driver := NewDriver(&imageregistryv1.ImageRegistryConfigStorageSwift{}, listers)
	conn, err := driver.getSwiftClient()
	if err != nil {
		if errors.As(err, &ErrContainerEndpointNotFound{}) {
			klog.Errorf("error connecting to Swift: %v", err)
			return false, nil
		}
		klog.Errorf("error connecting to OpenStack: %v", err)
		return false, err
	}

	// Try to list containers to make sure the user has required permissions to do that
	if err := containers.List(conn, containers.ListOpts{}).EachPage(func(_ pagination.Page) (bool, error) {
		return false, nil
	}); err != nil {
		klog.Errorf("error listing swift containers: %v", err)
		return false, nil
	}
	return true, nil
}

// GetConfig reads credentials
func GetConfig(listers *regopclient.StorageListers) (*Swift, error) {
	cfg := &Swift{}

	// Look for a user defined secret to get the Swift credentials
	sec, err := listers.Secrets.Get(defaults.ImageRegistryPrivateConfigurationUser)
	if err != nil && apimachineryerrors.IsNotFound(err) {
		// If no user defined credentials were provided, then try to find them in the secret,
		// created by cloud-credential-operator.
		sec, err = listers.Secrets.Get(defaults.CloudCredentialsName)
		if err != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials %q: %v", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.CloudCredentialsName), err)
		}

		// cloud-credential-operator is responsible for generating the clouds.yaml file and placing it in the local cloud creds secret.
		if cloudsData, ok := sec.Data["clouds.yaml"]; ok {
			var clouds clientconfig.Clouds
			err = yamlv2.Unmarshal(cloudsData, &clouds)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal clouds credentials: %v", err)
			}

			var cloudName string
			cloudInfra, err := util.GetInfrastructure(listers)
			if err != nil {
				if !apimachineryerrors.IsNotFound(err) {
					return nil, fmt.Errorf("failed to get cluster infrastructure info: %v", err)
				}
			}
			if cloudInfra != nil &&
				cloudInfra.Status.PlatformStatus != nil &&
				cloudInfra.Status.PlatformStatus.OpenStack != nil {
				cloudName = cloudInfra.Status.PlatformStatus.OpenStack.CloudName
			}
			if len(cloudName) == 0 {
				cloudName = "openstack"
			}

			if cloud, ok := clouds.Clouds[cloudName]; ok {
				cfg.AuthURL = cloud.AuthInfo.AuthURL
				cfg.Username = cloud.AuthInfo.Username
				cfg.Password = cloud.AuthInfo.Password
				cfg.ApplicationCredentialID = cloud.AuthInfo.ApplicationCredentialID
				cfg.ApplicationCredentialName = cloud.AuthInfo.ApplicationCredentialName
				cfg.ApplicationCredentialSecret = cloud.AuthInfo.ApplicationCredentialSecret
				cfg.Tenant = cloud.AuthInfo.ProjectName
				cfg.TenantID = cloud.AuthInfo.ProjectID
				cfg.Domain = cloud.AuthInfo.DomainName
				cfg.DomainID = cloud.AuthInfo.DomainID
				if cfg.Domain == "" {
					cfg.Domain = cloud.AuthInfo.UserDomainName
				}
				if cfg.DomainID == "" {
					cfg.DomainID = cloud.AuthInfo.UserDomainID
				}
				cfg.RegionName = cloud.RegionName
				cfg.IdentityAPIVersion = cloud.IdentityAPIVersion
			} else {
				return nil, fmt.Errorf("clouds.yaml does not contain required cloud \"openstack\"")
			}
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"clouds.yaml\"", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.CloudCredentialsName))
		}
	} else if err != nil {
		return nil, err
	} else {
		cfg.Username, err = util.GetValueFromSecret(sec, "REGISTRY_STORAGE_SWIFT_USERNAME")
		if err != nil {
			return nil, err
		}
		cfg.Password, err = util.GetValueFromSecret(sec, "REGISTRY_STORAGE_SWIFT_PASSWORD")
		if err != nil {
			return nil, err
		}
		cfg.ApplicationCredentialID, err = util.GetValueFromSecret(sec, "REGISTRY_STORAGE_SWIFT_APPLICATIONCREDENTIALID")
		if err != nil {
			return nil, err
		}
		cfg.ApplicationCredentialName, err = util.GetValueFromSecret(sec, "REGISTRY_STORAGE_SWIFT_APPLICATIONCREDENTIALNAME")
		if err != nil {
			return nil, err
		}
		cfg.ApplicationCredentialSecret, err = util.GetValueFromSecret(sec, "REGISTRY_STORAGE_SWIFT_APPLICATIONCREDENTIALSECRET")
		if err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// CABundle returns either the configured CA bundle or indicates that the
// system trust bundle should be used instead.
func (d *driver) CABundle() (string, bool, error) {
	cm, err := d.Listers.OpenShiftConfig.Get("cloud-provider-config")
	if apimachineryerrors.IsNotFound(err) {
		return "", true, nil
	}
	if err != nil {
		return "", false, err
	}
	caBundle := string(cm.Data["ca-bundle.pem"])
	if caBundle == "" {
		return "", true, nil
	}
	return caBundle, false, nil
}

type ErrContainerEndpointNotFound struct {
	wrapped error
}

func (err ErrContainerEndpointNotFound) Unwrap() error { return err.wrapped }
func (err ErrContainerEndpointNotFound) Error() string {
	return fmt.Sprintf("container endpoint not found in the OpenStack catalog: %v", err.wrapped)
}

// getSwiftClient returns a client that allows to interact with the OpenStack Swift service
func (d *driver) getSwiftClient() (*gophercloud.ServiceClient, error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	authURL := replaceEmpty(d.Config.AuthURL, cfg.AuthURL)
	tenant := replaceEmpty(d.Config.Tenant, cfg.Tenant)
	tenantID := replaceEmpty(d.Config.TenantID, cfg.TenantID)
	domain := replaceEmpty(d.Config.Domain, cfg.Domain)
	domainID := replaceEmpty(d.Config.DomainID, cfg.DomainID)
	regionName := replaceEmpty(d.Config.RegionName, cfg.RegionName)

	opts := &gophercloud.AuthOptions{
		IdentityEndpoint:            authURL,
		Username:                    cfg.Username,
		Password:                    cfg.Password,
		ApplicationCredentialID:     cfg.ApplicationCredentialID,
		ApplicationCredentialName:   cfg.ApplicationCredentialName,
		ApplicationCredentialSecret: cfg.ApplicationCredentialSecret,
		DomainID:                    domainID,
		DomainName:                  domain,
		TenantID:                    tenantID,
		TenantName:                  tenant,
	}

	provider, err := openstack.NewClient(opts.IdentityEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new OpenStack provider client: %w", err)
	}

	cert, _, err := d.CABundle()
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud provider CA certificate: %w", err)
	}

	if cert != "" {
		certPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("failed to read the system cert pool: %w", err)
		}
		certPool.AppendCertsFromPEM([]byte(cert))
		client := http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					RootCAs: certPool,
				},
			},
		}
		provider.HTTPClient = client
	}

	err = openstack.Authenticate(provider, *opts)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate against OpenStack: %w", err)
	}

	endpointOpts := gophercloud.EndpointOpts{
		Region: regionName,
		Name:   "swift",
	}

	client, err := openstack.NewContainerV1(provider, endpointOpts)
	if err != nil {
		if _, ok := err.(*gophercloud.ErrEndpointNotFound); ok {
			// In gophercloud the default endpoint type for
			// containers is "container". However, some OpenStack
			// clouds are deployed with a single endpoint type for
			// all Swift entities - "object-store".
			//
			// If a "container" endpoint is not found, then try
			// "object-store".
			endpointOpts.Type = "object-store"

			var errOnAlternativeEndpoint error
			client, errOnAlternativeEndpoint = openstack.NewContainerV1(provider, endpointOpts)
			if errOnAlternativeEndpoint != nil {
				if _, ok := errOnAlternativeEndpoint.(*gophercloud.ErrEndpointNotFound); ok {
					// If none of the endpoints is found, then
					// return the error we got on the default one
					// so that we limit confusion on the error
					// trace.
					return nil, ErrContainerEndpointNotFound{err}
				} else {
					return nil, fmt.Errorf("failed to get the object storage alternative endpoint: %w", errOnAlternativeEndpoint)
				}
			}
			return client, nil
		}
		return nil, fmt.Errorf("failed to get the object storage endpoint: %w", err)
	}

	return client, nil
}

// NewDriver creates new Swift driver for the Image Registry
func NewDriver(c *imageregistryv1.ImageRegistryConfigStorageSwift, listers *regopclient.StorageListers) *driver {
	return &driver{
		Config:  c,
		Listers: listers,
	}
}

func (d *driver) ConfigEnv() (envs envvar.List, err error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	authURL := replaceEmpty(d.Config.AuthURL, cfg.AuthURL)
	tenant := replaceEmpty(d.Config.Tenant, cfg.Tenant)
	tenantID := replaceEmpty(d.Config.TenantID, cfg.TenantID)
	domain := replaceEmpty(d.Config.Domain, cfg.Domain)
	domainID := replaceEmpty(d.Config.DomainID, cfg.DomainID)
	regionName := replaceEmpty(d.Config.RegionName, cfg.RegionName)
	authVersionStr := replaceEmpty(d.Config.AuthVersion, cfg.IdentityAPIVersion)
	authVersionStr = replaceEmpty(authVersionStr, "3")

	authVersion, err := strconv.Atoi(authVersionStr)
	if err != nil {
		return nil, fmt.Errorf("unable to parse authVersion: %s", err)
	}

	authURL, err = ensureAuthURLHasAPIVersion(authURL, authVersionStr)
	if err != nil {
		return nil, err
	}

	envs = append(envs,
		envvar.EnvVar{Name: "REGISTRY_STORAGE", Value: "swift"},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_CONTAINER", Value: d.Config.Container},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_AUTHURL", Value: authURL},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_USERNAME", Value: cfg.Username, Secret: true},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_PASSWORD", Value: cfg.Password, Secret: true},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_APPLICATIONCREDENTIALID", Value: cfg.ApplicationCredentialID, Secret: true},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_APPLICATIONCREDENTIALNAME", Value: cfg.ApplicationCredentialName, Secret: true},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_APPLICATIONCREDENTIALSECRET", Value: cfg.ApplicationCredentialSecret, Secret: true},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_AUTHVERSION", Value: authVersion},
	)
	if domain != "" {
		envs = append(envs, envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_DOMAIN", Value: domain})
	}
	if domainID != "" {
		envs = append(envs, envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_DOMAINID", Value: domainID})
	}
	if tenant != "" {
		envs = append(envs, envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_TENANT", Value: tenant})
	}
	if tenantID != "" {
		envs = append(envs, envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_TENANTID", Value: tenantID})
	}
	if regionName != "" {
		envs = append(envs, envvar.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_REGION", Value: regionName})
	}

	return
}

func ensureAuthURLHasAPIVersion(authURL, authVersion string) (string, error) {
	authURL, err := urlx.NormalizeString(authURL)
	if err != nil {
		return "", err
	}

	parsedURL, err := urlx.Parse(authURL)
	if err != nil {
		return "", err
	}

	path := parsedURL.Path

	// check if authUrl contains API version
	if strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/v2") || strings.HasPrefix(path, "/v3") {
		return authURL, nil
	}

	// check that path is empty
	if !(path == "/" || path == "") {
		return "", fmt.Errorf("Incorrect Auth URL: %s", path)
	}

	// append trailing / to the url
	if !strings.HasSuffix(authURL, "/") {
		authURL = authURL + "/"
	}

	return authURL + "v" + authVersion, nil
}

func (d *driver) containerExists(client *gophercloud.ServiceClient, containerName string) error {
	_, err := containers.Get(client, containerName, containers.GetOpts{}).Extract()
	return err
}

func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	client, err := d.getSwiftClient()
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Could not connect to registry storage", err.Error())
		return false, err
	}

	err = d.containerExists(client, cr.Spec.Storage.Swift.Container)
	if err != nil {
		if serr, ok := err.(*gophercloud.ErrResourceNotFound); ok {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Storage does not exist", serr.Error())
			return false, nil
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Unknown error occurred", err.Error())
		return false, err
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Swift container Exists", "")
	return true, nil
}

func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.Swift, cr.Spec.Storage.Swift) {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Swift Configuration Changed", "Swift storage is in an unknown state")
		return true
	}

	return false
}

func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	client, err := d.getSwiftClient()
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, err.Error(), err.Error())
		return err
	}

	infra, err := util.GetInfrastructure(d.Listers)
	if err != nil {
		return fmt.Errorf("failed to get cluster infrastructure info: %v", err)
	}

	generatedName := false
	const numRetries = 5000
	for i := 0; i < numRetries; i++ {
		if len(cr.Spec.Storage.Swift.Container) == 0 {
			if cr.Spec.Storage.Swift.Container, err = util.GenerateStorageName(d.Listers, ""); err != nil {
				return err
			}
			generatedName = true
		}

		err = d.containerExists(client, cr.Spec.Storage.Swift.Container)
		if err != nil {
			// If the error is not ErrResourceNotFound
			// return the error
			if _, ok := err.(gophercloud.ErrDefault404); !ok {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Unable to check if container exists", fmt.Sprintf("Error occurred checking if container exists: %v", err))
				return err
			}
			// If the error is ErrResourceNotFound
			// fall through to the container creation
		}
		// If we were supplied a container name and it exists
		// we can skip the create
		if !generatedName && err == nil {
			if cr.Spec.Storage.ManagementState == "" {
				cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateUnmanaged
			}
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Container exists", "User supplied container already exists")
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				Swift: d.Config.DeepCopy(),
			}
			break
		}
		// If we generated a container name and it exists
		// let's try again
		if generatedName && err == nil {
			cr.Spec.Storage.Swift.Container = ""
			continue
		}

		createOps := containers.CreateOpts{
			Metadata: map[string]string{
				"Openshiftclusterid": infra.Status.InfrastructureName,
				"Name":               cr.Spec.Storage.Swift.Container,
			},
		}

		_, err = containers.Create(client, cr.Spec.Storage.Swift.Container, createOps).Extract()
		if err != nil {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Creation Failed", err.Error())
			return err
		}

		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Swift Container Created", "")

		if cr.Spec.Storage.ManagementState == "" {
			cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateManaged
		}
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			Swift: d.Config.DeepCopy(),
		}
		cr.Spec.Storage.Swift = d.Config.DeepCopy()

		break
	}

	return nil
}

func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (bool, error) {
	if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged ||
		cr.Spec.Storage.Swift.Container == "" {
		return false, nil
	}

	client, err := d.getSwiftClient()
	if err != nil {
		return false, err
	}

	pager := objects.List(client, cr.Spec.Storage.Swift.Container, &objects.ListOpts{
		Limit: 50,
	})
	if err := pager.EachPage(func(page pagination.Page) (bool, error) {
		objectsOnPage, err := objects.ExtractNames(page)
		if err != nil {
			return false, err
		}
		resp, err := objects.BulkDelete(client, cr.Spec.Storage.Swift.Container, objectsOnPage).Extract()
		if err != nil {
			return false, err
		}
		if len(resp.Errors) > 0 {
			// Convert resp.Errors to golang errors.
			// Each error is represented by a list of 2 strings, where the first one
			// is the object name, and the second one contains an error message.
			errs := make([]error, len(resp.Errors))
			for i, objectError := range resp.Errors {
				errs[i] = fmt.Errorf("cannot delete object %v: %v", objectError[0], objectError[1])
			}

			return false, fmt.Errorf("errors occurred during bulk deleting of container %v objects: %v", cr.Spec.Storage.Swift.Container, k8sutilerrors.NewAggregate(errs))
		}

		return true, nil
	}); err != nil {
		if _, ok := err.(gophercloud.ErrDefault404); !ok {
			return false, err
		}
	}

	_, err = containers.Delete(client, cr.Spec.Storage.Swift.Container).Extract()
	if err != nil {
		if _, ok := err.(gophercloud.ErrDefault404); !ok {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, err.Error(), err.Error())
			return false, err
		}
	}

	cr.Spec.Storage.Swift.Container = ""
	d.Config.Container = ""

	if !reflect.DeepEqual(cr.Status.Storage.Swift, d.Config) {
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			Swift: d.Config.DeepCopy(),
		}
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Swift Container Deleted", "The swift container has been removed.")

	return true, nil
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	return nil, nil, nil
}

func (d *driver) VolumeSecrets() (map[string]string, error) {
	return nil, nil
}

// ID return the underlying storage identificator, on this case the Swift
// container name.
func (d *driver) ID() string {
	return d.Config.Container
}
