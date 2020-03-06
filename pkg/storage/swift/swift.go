package swift

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/objectstorage/v1/containers"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/goware/urlx"
	yamlv2 "gopkg.in/yaml.v2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

type Swift struct {
	AuthURL            string
	Username           string
	Password           string
	Tenant             string
	TenantID           string
	Domain             string
	DomainID           string
	RegionName         string
	IdentityAPIVersion string
}

type driver struct {
	// Config is a struct where the basic configuration is stored
	Config *imageregistryv1.ImageRegistryConfigStorageSwift
	// Listers are used to download OpenStack credentials from the native secret
	Listers *regopclient.Listers
}

// replaceEmpty is a helper function to replace empty fields with another field
func replaceEmpty(a string, b string) string {
	if a == "" {
		return b
	}
	return a
}

// GetConfig reads credentials
func GetConfig(listers *regopclient.Listers) (*Swift, error) {
	cfg := &Swift{}

	// Look for a user defined secret to get the Swift credentials
	sec, err := listers.Secrets.Get(defaults.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
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
			cloudInfra, err := listers.Infrastructures.Get("cluster")
			if err != nil {
				if !errors.IsNotFound(err) {
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
	}

	return cfg, nil
}

func getCloudProviderCert(listers *regopclient.Listers) (string, error) {
	cm, err := listers.OpenShiftConfig.Get("cloud-provider-config")
	if err != nil {
		return "", err
	}
	return string(cm.Data["ca-bundle.pem"]), nil
}

// getSwiftClient returns a client that allows to interact with the OpenStack Swift service
func (d *driver) getSwiftClient(cr *imageregistryv1.Config) (*gophercloud.ServiceClient, error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	d.Config.AuthURL = replaceEmpty(d.Config.AuthURL, cfg.AuthURL)
	d.Config.Tenant = replaceEmpty(d.Config.Tenant, cfg.Tenant)
	d.Config.TenantID = replaceEmpty(d.Config.TenantID, cfg.TenantID)
	d.Config.Domain = replaceEmpty(d.Config.Domain, cfg.Domain)
	d.Config.DomainID = replaceEmpty(d.Config.DomainID, cfg.DomainID)
	d.Config.RegionName = replaceEmpty(d.Config.RegionName, cfg.RegionName)
	d.Config.AuthVersion = replaceEmpty(d.Config.AuthVersion, cfg.IdentityAPIVersion)
	d.Config.AuthVersion = replaceEmpty(d.Config.AuthVersion, "3")

	opts := &gophercloud.AuthOptions{
		IdentityEndpoint: d.Config.AuthURL,
		Username:         cfg.Username,
		Password:         cfg.Password,
		DomainID:         d.Config.DomainID,
		DomainName:       d.Config.Domain,
		TenantID:         d.Config.TenantID,
		TenantName:       d.Config.Tenant,
	}

	provider, err := openstack.NewClient(opts.IdentityEndpoint)
	if err != nil {
		return nil, fmt.Errorf("Create new provider client failed: %v", err)
	}

	cert, err := getCloudProviderCert(d.Listers)
	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("Failed to get cloud provider CA certificate: %v", err)
	}

	if cert != "" {
		certPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("Create system cert pool failed: %v", err)
		}
		certPool.AppendCertsFromPEM([]byte(cert))
		client := http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: certPool,
				},
			},
		}
		provider.HTTPClient = client
	}

	err = openstack.Authenticate(provider, *opts)
	if err != nil {
		return nil, fmt.Errorf("Failed to authenticate provider client: %v", err)
	}

	endpointOpts := gophercloud.EndpointOpts{
		Region: d.Config.RegionName,
		Name:   "swift",
	}

	var client *gophercloud.ServiceClient
	client, err = openstack.NewContainerV1(provider, endpointOpts)
	if err != nil {
		if _, ok := err.(*gophercloud.ErrEndpointNotFound); ok {
			endpointOpts.Type = "object-store"
			client, err = openstack.NewContainerV1(provider, endpointOpts)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return client, nil
}

// NewDriver creates new Swift driver for the Image Registry
func NewDriver(c *imageregistryv1.ImageRegistryConfigStorageSwift, listers *regopclient.Listers) *driver {
	return &driver{
		Config:  c,
		Listers: listers,
	}
}

// Secrets returns a map of the storage access secrets
func (d *driver) Secrets() (map[string]string, error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"REGISTRY_STORAGE_SWIFT_USERNAME": cfg.Username,
		"REGISTRY_STORAGE_SWIFT_PASSWORD": cfg.Password,
	}, nil
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	d.Config.AuthURL = replaceEmpty(d.Config.AuthURL, cfg.AuthURL)
	d.Config.Tenant = replaceEmpty(d.Config.Tenant, cfg.Tenant)
	d.Config.TenantID = replaceEmpty(d.Config.TenantID, cfg.TenantID)
	d.Config.Domain = replaceEmpty(d.Config.Domain, cfg.Domain)
	d.Config.DomainID = replaceEmpty(d.Config.DomainID, cfg.DomainID)
	d.Config.RegionName = replaceEmpty(d.Config.RegionName, cfg.RegionName)
	d.Config.AuthVersion = replaceEmpty(d.Config.AuthVersion, cfg.IdentityAPIVersion)
	d.Config.AuthVersion = replaceEmpty(d.Config.AuthVersion, "3")

	err = d.ensureAuthURLHasAPIVersion()
	if err != nil {
		return nil, err
	}

	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: "swift"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_CONTAINER", Value: d.Config.Container},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_AUTHURL", Value: d.Config.AuthURL},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_SWIFT_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: defaults.ImageRegistryPrivateConfiguration,
					},
					Key: "REGISTRY_STORAGE_SWIFT_USERNAME",
				},
			},
		},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_SWIFT_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: defaults.ImageRegistryPrivateConfiguration,
					},
					Key: "REGISTRY_STORAGE_SWIFT_PASSWORD",
				},
			},
		},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_AUTHVERSION", Value: d.Config.AuthVersion},
	)
	if d.Config.Domain != "" {
		envs = append(envs, corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_DOMAIN", Value: d.Config.Domain})
	}
	if d.Config.DomainID != "" {
		envs = append(envs, corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_DOMAINID", Value: d.Config.DomainID})
	}
	if d.Config.Tenant != "" {
		envs = append(envs, corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_TENANT", Value: d.Config.Tenant})
	}
	if d.Config.TenantID != "" {
		envs = append(envs, corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_TENANTID", Value: d.Config.TenantID})
	}
	if d.Config.RegionName != "" {
		envs = append(envs, corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_REGION", Value: d.Config.RegionName})
	}

	return
}

func (d *driver) ensureAuthURLHasAPIVersion() error {
	authURL, err := urlx.NormalizeString(d.Config.AuthURL)
	if err != nil {
		return err
	}

	authVersion := d.Config.AuthVersion

	parsedURL, err := urlx.Parse(authURL)
	if err != nil {
		return err
	}

	path := parsedURL.Path

	// check if authUrl contains API version
	if strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/v2") || strings.HasPrefix(path, "/v3") {
		d.Config.AuthURL = authURL
		return nil
	}

	// check that path is empty
	if !(path == "/" || path == "") {
		return fmt.Errorf("Incorrect Auth URL: %s", path)
	}

	// append trailing / to the url
	if !strings.HasSuffix(d.Config.AuthURL, "/") {
		authURL = authURL + "/"
	}

	d.Config.AuthURL = authURL + "v" + authVersion

	return nil
}

func (d *driver) containerExists(client *gophercloud.ServiceClient, containerName string) error {
	if len(containerName) == 0 {
		return nil
	}
	_, err := containers.Get(client, containerName, containers.GetOpts{}).Extract()

	return err

}

func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	client, err := d.getSwiftClient(cr)
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
	client, err := d.getSwiftClient(cr)
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, err.Error(), err.Error())
		return err
	}

	infra, err := d.Listers.Infrastructures.Get("cluster")
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
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Container exists", "User supplied container already exists")
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
			cr.Status.StorageManaged = false
			return err
		}

		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Swift Container Created", "")

		cr.Status.StorageManaged = true
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			Swift: d.Config.DeepCopy(),
		}
		cr.Spec.Storage.Swift = d.Config.DeepCopy()

		break
	}

	return nil
}

func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (bool, error) {
	if !cr.Status.StorageManaged {
		return false, nil
	}

	client, err := d.getSwiftClient(cr)
	if err != nil {
		return false, err
	}

	_, err = containers.Delete(client, cr.Spec.Storage.Swift.Container).Extract()
	if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, err.Error(), err.Error())
		return false, err
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

// ID return the underlying storage identificator, on this case the Swift
// container name.
func (d *driver) ID() string {
	return d.Config.Container
}
