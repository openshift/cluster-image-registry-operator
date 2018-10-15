package filesystem

import (
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

const (
	rootDirectory = "/registry"
)

type driver struct {
	Config *opapi.ImageRegistryConfigStorageFilesystem
}

func NewDriver(c *opapi.ImageRegistryConfigStorageFilesystem) *driver {
	return &driver{
		Config: c,
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

	fieldname, err := getVolumeSourceField(&d.Config.VolumeSource)
	if err != nil {
		return err
	}

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

func getVolumeSourceField(source *corev1.VolumeSource) (string, error) {
	val := reflect.Indirect(reflect.ValueOf(source))

	n := 0
	name := ""

	for i := 0; i < val.NumField(); i++ {
		if !val.Field(i).IsNil() {
			name = val.Type().Field(i).Name
			n += 1
		}
	}

	switch n {
	case 0:
		// volumeSource is not configured yet.
		return "", nil
	case 1:
		return strings.ToLower(name), nil
	}

	return "", fmt.Errorf("too many storage types defined")
}
