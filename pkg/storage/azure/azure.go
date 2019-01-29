package azure

import (
	corev1 "k8s.io/api/core/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
)

type driver struct {
	Config *imageregistryv1.ImageRegistryConfigStorageAzure
}

func NewDriver(c *imageregistryv1.ImageRegistryConfigStorageAzure) *driver {
	return &driver{
		Config: c,
	}
}

func (d *driver) Secrets() (map[string]string, error) {
	return nil, nil
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: "azure"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_AZURE_CONTAINER", Value: d.Config.Container},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_AZURE_ACCOUNTNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: imageregistryv1.ImageRegistryPrivateConfiguration,
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
						Name: imageregistryv1.ImageRegistryPrivateConfiguration,
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
