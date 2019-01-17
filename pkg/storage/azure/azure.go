package azure

import (
	coreapi "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
)

type driver struct {
	Name      string
	Namespace string
	Config    *imageregistryv1.ImageRegistryConfigStorageAzure
}

func NewDriver(crname string, crnamespace string, c *imageregistryv1.ImageRegistryConfigStorageAzure) *driver {
	return &driver{
		Name:      crname,
		Namespace: crnamespace,
		Config:    c,
	}
}

func (d *driver) UpdateFromStorage(cfg imageregistryv1.ImageRegistryConfigStorage) {
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
						Name: d.Name + "-private-configuration",
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
						Name: d.Name + "-private-configuration",
					},
					Key: "REGISTRY_STORAGE_AZURE_ACCOUNTKEY",
				},
			},
		},
	)

	return
}

func (d *driver) StorageExists(cr *imageregistryv1.Config, modified *bool) (bool, error) {
	return false, nil
}

func (d *driver) StorageChanged(cr *imageregistryv1.Config, modified *bool) bool {
	return false
}

func (d *driver) GetStorageName() string {
	if d.Config == nil {
		return ""
	}
	return d.Config.Container
}

func (d *driver) CreateStorage(cr *imageregistryv1.Config, modified *bool) error {
	return nil
}

func (d *driver) RemoveStorage(cr *imageregistryv1.Config, modified *bool) (bool, error) {
	if !cr.Status.StorageManaged {
		return false, nil
	}

	return false, nil
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	return nil, nil, nil
}

func (d *driver) CompleteConfiguration(cr *imageregistryv1.Config, modified *bool) error {
	return nil
}
