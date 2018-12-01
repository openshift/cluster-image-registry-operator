package emptydir

import (
	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

const (
	rootDirectory = "/registry"
)

type driver struct {
	Name      string
	Namespace string
}

func NewDriver(crname string, crnamespace string, c *opapi.ImageRegistryConfigStorageFilesystem) *driver {
	return &driver{
		Name:      crname,
		Namespace: crnamespace,
	}
}

func (d *driver) GetName() string {
	return "emptydir"
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
		Name: "registry-storage",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	mount := corev1.VolumeMount{
		Name:      vol.Name,
		MountPath: rootDirectory,
	}

	return []corev1.Volume{vol}, []corev1.VolumeMount{mount}, nil
}

func (d *driver) CompleteConfiguration(customResourceStatus *opapi.ImageRegistryStatus) error {
	return nil
}
