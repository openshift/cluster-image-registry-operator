package emptydir

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
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

func (d *driver) CompleteConfiguration() error {
	return nil
}

func (d *driver) ValidateConfiguration(cr *opapi.ImageRegistry, modified *bool) error {
	if v, ok := util.GetStateValue(&cr.Status, "storagetype"); ok {
		if v != d.GetName() {
			return fmt.Errorf("storage type change is not supported: expected storage type %s, but got %s", v, d.GetName())
		}
	} else {
		util.SetStateValue(&cr.Status, "storagetype", d.GetName())
		*modified = true
	}

	if v, ok := util.GetStateValue(&cr.Status, "storagefield"); ok {
		if v != "emptydir" {
			return fmt.Errorf("volumeSource type change is not supported: expected storage type %s, but got emptydir", v)
		}
	} else {
		util.SetStateValue(&cr.Status, "storagefield", "emptydir")
		*modified = true
	}

	return nil
}
