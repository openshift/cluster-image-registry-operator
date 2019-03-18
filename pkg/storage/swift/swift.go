package swift

import (
	"fmt"
	"math/rand"
	"reflect"

	corev1 "k8s.io/api/core/v1"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/objectstorage/v1/containers"
	"github.com/gophercloud/utils/openstack/clientconfig"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"

	yaml "gopkg.in/yaml.v2"
)

type driver struct {
	// Config is a struct where the basic configuration is stored
	Config *imageregistryv1.ImageRegistryConfigStorageSwift
	// Listers are used to download OpenStack credentials from the native secret
	Listers *regopclient.Listers
}

const (
	cloudSecretName = "openstack-cloud"
	cloudSecretKey  = "clouds.yaml"
	cloudName       = "openshift"
)

// getCloudFromSecret allows reading OpenStack cloud config from a k8s secret
func (d *driver) getCloudFromSecret() (clientconfig.Cloud, error) {
	emptyCloud := clientconfig.Cloud{}

	// Look for a user defined secret to get the Swift credentials
	secret, err := d.Listers.InstallerSecrets.Get(cloudSecretName)
	if err != nil {
		return emptyCloud, err
	}

	content, ok := secret.Data[cloudSecretKey]
	if !ok {
		return emptyCloud, fmt.Errorf("OpenStack credentials secret %v did not contain key %v",
			cloudSecretName, cloudSecretKey)
	}

	var clouds clientconfig.Clouds
	err = yaml.Unmarshal(content, &clouds)
	if err != nil {
		return emptyCloud, fmt.Errorf("failed to unmarshal clouds credentials stored in secret %v: %v", cloudSecretName, err)
	}

	return clouds.Clouds[cloudName], nil
}

// getSwiftClient returns a client that allows us to interact with the OpenStack Swift service
// If CloudsSecret and CloudsName parameters are provided in the operator's config, then it will
// try to read cloud config from the related secret and use it, overwriting all other parameters
// in the operator's config (Domain, Tenant, AuthURL, etc.).
// If those two parameters are omitted, then credentials will be taken from the operator's native
// secret.
func (d *driver) getSwiftClient(cr *imageregistryv1.Config) (*gophercloud.ServiceClient, error) {
	clientOpts := new(clientconfig.ClientOpts)
	var opts *gophercloud.AuthOptions

	if cr.Spec.Storage.Swift.UseGlobalConfig {
		var err error
		// If CloudsSecret and CloudsName parameters are provided then read config from clouds.yaml
		cloud, err := d.getCloudFromSecret()
		if err != nil {
			return nil, err
		}

		if cloud.AuthInfo != nil {
			clientOpts.AuthInfo = cloud.AuthInfo
			clientOpts.AuthType = cloud.AuthType
			clientOpts.Cloud = cloud.Cloud
			clientOpts.RegionName = cloud.RegionName
		}

		opts, err = clientconfig.AuthOptions(clientOpts)
		if err != nil {
			return nil, err
		}
	} else {
		// Otherwise take credentials from the native secret
		cfg, err := clusterconfig.GetSwiftConfig(d.Listers)
		if err != nil {
			return nil, err
		}

		opts = &gophercloud.AuthOptions{
			IdentityEndpoint: cr.Spec.Storage.Swift.AuthURL,
			Username:         cfg.Storage.Swift.Username,
			Password:         cfg.Storage.Swift.Password,
			DomainID:         cr.Spec.Storage.Swift.DomainID,
			DomainName:       cr.Spec.Storage.Swift.Domain,
			TenantID:         cr.Spec.Storage.Swift.TenantID,
			TenantName:       cr.Spec.Storage.Swift.Tenant,
		}
	}

	opts.AllowReauth = true

	provider, err := openstack.AuthenticatedClient(*opts)
	if err != nil {
		return nil, err
	}

	client, err := openstack.NewContainerV1(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, err
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
	if !d.Config.UseGlobalConfig {
		cfg, err := clusterconfig.GetSwiftConfig(d.Listers)
		if err != nil {
			return nil, err
		}

		return map[string]string{
			"REGISTRY_STORAGE_SWIFT_USERNAME": cfg.Storage.Swift.Username,
			"REGISTRY_STORAGE_SWIFT_PASSWORD": cfg.Storage.Swift.Password,
		}, nil
	}

	return nil, nil
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	// Add common values that do not depend on the config source
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: "swift"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_CONTAINER", Value: d.Config.Container},
	)
	if d.Config.UseGlobalConfig {
		// If these values are provided read remaining values from the cloud config
		cloud, err := d.getCloudFromSecret()
		if err != nil {
			return nil, err
		}

		envs = append(envs,
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_AUTHURL", Value: cloud.AuthInfo.AuthURL},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_DOMAIN", Value: cloud.AuthInfo.DomainName},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_DOMAINID", Value: cloud.AuthInfo.DomainID},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_TENANT", Value: cloud.AuthInfo.ProjectName},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_TENANTID", Value: cloud.AuthInfo.ProjectID},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_USERNAME", Value: cloud.AuthInfo.Username},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_PASSWORD", Value: cloud.AuthInfo.Password},
		)
	} else {
		// Otherwise read values from the local config, including credentials from the secret
		envs = append(envs,
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_AUTHURL", Value: d.Config.AuthURL},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_DOMAIN", Value: d.Config.Domain},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_DOMAINID", Value: d.Config.DomainID},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_TENANT", Value: d.Config.Tenant},
			corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_TENANTID", Value: d.Config.TenantID},
			corev1.EnvVar{
				Name: "REGISTRY_STORAGE_SWIFT_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: imageregistryv1.ImageRegistryPrivateConfiguration,
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
							Name: imageregistryv1.ImageRegistryPrivateConfiguration,
						},
						Key: "REGISTRY_STORAGE_SWIFT_PASSWORD",
					},
				},
			},
		)
	}
	return
}

func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	client, err := d.getSwiftClient(cr)
	if err != nil {
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionUnknown, err.Error(), err.Error())
		return false, err
	}

	_, err = containers.Get(client, cr.Spec.Storage.Swift.Container, containers.GetOpts{}).Extract()
	if err != nil {
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, err.Error(), err.Error())
		return false, err
	}

	util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionTrue, "Swift container Exists", "")
	return true, nil
}

func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.Swift, cr.Spec.Storage.Swift) {
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionUnknown, "Swift Configuration Changed", "Swift storage is in an unknown state")
		return true
	}

	return false
}

func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	client, err := d.getSwiftClient(cr)
	if err != nil {
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, err.Error(), err.Error())
		return err
	}

	// Generate new container name if it wasn't provided.
	// The name has a prefix "image_registry_", which is complemented by 16 capital latin letters
	// Example of a generated name: image_registry_FHEIBGDDGBLWPXFR
	if cr.Spec.Storage.Swift.Container == "" {
		bytes := make([]byte, 16)
		for i := 0; i < 16; i++ {
			bytes[i] = byte(65 + rand.Intn(25)) // A=65 and Z=65+25
		}
		cr.Spec.Storage.Swift.Container = "image_registry_" + string(bytes)
	}

	fmt.Println(cr.Spec.Storage.Swift.Container)

	_, err = containers.Create(client, cr.Spec.Storage.Swift.Container, containers.CreateOpts{}).Extract()
	if err != nil {
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, "Creation Failed", err.Error())
		cr.Status.StorageManaged = false
		return err
	}

	util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionTrue, "Swift Container Created", "")

	cr.Status.StorageManaged = true
	cr.Status.Storage.Swift = d.Config.DeepCopy()
	cr.Spec.Storage.Swift = d.Config.DeepCopy()

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
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionUnknown, err.Error(), err.Error())
		return false, err
	}

	cr.Spec.Storage.Swift.Container = ""
	d.Config.Container = ""

	if !reflect.DeepEqual(cr.Status.Storage.Swift, d.Config) {
		cr.Status.Storage.Swift = d.Config.DeepCopy()
	}

	util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, "Swift Container Deleted", "The swift container has been removed.")

	return true, nil
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	return nil, nil, nil
}

func (d *driver) CompleteConfiguration(cr *imageregistryv1.Config) error {
	cr.Status.Storage.Swift = d.Config.DeepCopy()
	return nil
}
