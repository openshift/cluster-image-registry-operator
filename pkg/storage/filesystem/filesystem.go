package filesystem

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/coreutil"
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
	return "filesystem"
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

func (d *driver) CompleteConfiguration() error {
	return nil
}

func (d *driver) ValidateConfiguration(prevState *corev1.ConfigMap) error {
	if v, ok := prevState.Data["storagetype"]; ok {
		if v != d.GetName() {
			return fmt.Errorf("storage type change is not supported: expected storage type %s, but got %s", v, d.GetName())
		}
	} else {
		prevState.Data["storagetype"] = d.GetName()
	}

	field, err := coreutil.GetVolumeSourceField(d.Config.VolumeSource)
	if err != nil {
		return err
	}

	fieldname := strings.ToLower(field.Name)

	if len(fieldname) > 0 {
		if v, ok := prevState.Data["storagefield"]; ok {
			if v != fieldname {
				return fmt.Errorf("volumeSource type change is not supported: expected storage type %s, but got %s", v, fieldname)
			}
		} else {
			prevState.Data["storagefield"] = fieldname
		}
	}

	return nil
}
