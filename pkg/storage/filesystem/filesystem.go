package filesystem

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
)

const (
	rootDirectory = "/registry"
)

type driver struct {
	Name      string
	Namespace string
	Config    *opapi.ImageRegistryConfigStorageFilesystem
}

func NewDriver(crname string, crnamespace string, c *opapi.ImageRegistryConfigStorageFilesystem) *driver {
	return &driver{
		Name:      crname,
		Namespace: crnamespace,
		Config:    c,
	}
}

func (d *driver) GetName() string {
	return string(clusterconfig.StorageTypeFileSystem)
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetName()},
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

func (d *driver) StorageExists(cr *opapi.ImageRegistry) error {
	return nil
}

func (d *driver) CreateStorage(cr *opapi.ImageRegistry) error {
	return nil
}

func (d *driver) RemoveStorage(cr *opapi.ImageRegistry) error {
	if !cr.Status.Storage.Managed {
		return fmt.Errorf("storage is not managed by the image registry operator, so we can't delete it.")
	}

	return nil
}

func (d *driver) CompleteConfiguration(cr *opapi.ImageRegistry) error {
	cr.Status.Storage.State.Filesystem = d.Config

	return nil
}
