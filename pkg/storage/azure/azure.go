package azure

import (
	"fmt"

	coreapi "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
)

type driver struct {
	Name      string
	Namespace string
	Config    *opapi.ImageRegistryConfigStorageAzure
}

func NewDriver(crname string, crnamespace string, c *opapi.ImageRegistryConfigStorageAzure) *driver {
	return &driver{
		Name:      crname,
		Namespace: crnamespace,
		Config:    c,
	}
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

func (d *driver) StorageExists(cr *opapi.ImageRegistry, modified *bool) (bool, error) {
	return false, nil
}

func (d *driver) StorageChanged(cr *opapi.ImageRegistry, modified *bool) bool {
	return false
}

func (d *driver) GetStorageName(cr *opapi.ImageRegistry, modified *bool) (string, error) {
	if cr.Spec.Storage.Azure != nil {
		return cr.Spec.Storage.Azure.Container, nil
	}
	return "", fmt.Errorf("unable to retrieve container name from image registry resource: %#v", cr.Spec.Storage)
}

func (d *driver) CreateStorage(cr *opapi.ImageRegistry, modified *bool) error {
	return nil
}

func (d *driver) RemoveStorage(cr *opapi.ImageRegistry, modified *bool) error {
	if !cr.Status.Storage.Managed {
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
