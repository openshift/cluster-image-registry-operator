package filesystem

import (
	corev1 "k8s.io/api/core/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
)

const (
	rootDirectory = "/registry"
)

type driver struct {
	Config *imageregistryv1.ImageRegistryConfigStorageFilesystem
}

func NewDriver(c *imageregistryv1.ImageRegistryConfigStorageFilesystem) *driver {
	return &driver{
		Config: c,
	}
}

func (d *driver) UpdateFromStorage(cfg imageregistryv1.ImageRegistryConfigStorage) {
	d.Config = cfg.Filesystem.DeepCopy()
}

func (d *driver) Secrets() (map[string]string, error) {
	return nil, nil
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: "filesystem"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY", Value: rootDirectory},
	)

	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	vol := corev1.Volume{
		Name:         "registry-storage",
		VolumeSource: d.Config.VolumeSource,
	}

	mount := corev1.VolumeMount{
		Name:      vol.Name,
		MountPath: rootDirectory,
	}

	return []corev1.Volume{vol}, []corev1.VolumeMount{mount}, nil
}

func (d *driver) StorageExists(cr *imageregistryv1.Config, modified *bool) (bool, error) {
	return false, nil
}

func (d *driver) StorageChanged(cr *imageregistryv1.Config, modified *bool) bool {
	return false
}

func (d *driver) GetStorageName() string {
	return ""
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

func (d *driver) CompleteConfiguration(cr *imageregistryv1.Config, modified *bool) error {
	cr.Status.Storage.Filesystem = d.Config.DeepCopy()
	*modified = true
	return nil
}
