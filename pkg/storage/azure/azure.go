package azure

import (
	coreapi "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
)

type driver struct {
	Config    *opapi.ImageRegistryConfigStorageAzure
}

func NewDriver(c *opapi.ImageRegistryConfigStorageAzure) *driver {
	return &driver{
		Config:    c,
	}
}

func (d *driver) UpdateFromStorage(cfg opapi.ImageRegistryConfigStorage) {
	d.Config = cfg.Azure.DeepCopy()
}

func (d *driver) GetType() string {
	return string(clusterconfig.StorageTypeAzure)
}

func (d *driver) SyncSecrets(sec *coreapi.Secret) (map[string]string, error) {
	return nil, nil
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetType()},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_AZURE_CONTAINER", Value: d.Config.Container},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_AZURE_ACCOUNTNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: opapi.ImageRegistryPrivateConfiguration,
					},
					Key: "REGISTRY_STORAGE_AZURE_ACCOUNTNAME",
				},
			},
		},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_AZURE_ACCOUNTKEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: opapi.ImageRegistryPrivateConfiguration,
					},
					Key: "REGISTRY_STORAGE_AZURE_ACCOUNTKEY",
				},
			},
		},
	)

	return
}

func (d *driver) StorageExists(cr *opapi.ImageRegistry, modified *bool) (bool, error) {
	return false, nil
}

func (d *driver) StorageChanged(cr *opapi.ImageRegistry, modified *bool) bool {
	return false
}

func (d *driver) GetStorageName() string {
	if d.Config == nil {
		return ""
	}
	return d.Config.Container
}

func (d *driver) CreateStorage(cr *opapi.ImageRegistry, modified *bool) error {
	return nil
}

func (d *driver) RemoveStorage(cr *opapi.ImageRegistry, modified *bool) error {
	if !cr.Status.StorageManaged {
		return nil
	}

	return nil
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	return nil, nil, nil
}

func (d *driver) CompleteConfiguration(cr *opapi.ImageRegistry, modified *bool) error {
	return nil
}
