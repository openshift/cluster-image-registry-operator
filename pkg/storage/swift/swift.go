package swift

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
)

type driver struct {
	Name      string
	Namespace string
	Config    *opapi.ImageRegistryConfigStorageSwift
}

func NewDriver(crname string, crnamespace string, c *opapi.ImageRegistryConfigStorageSwift) *driver {
	return &driver{
		Name:      crname,
		Namespace: crnamespace,
		Config:    c,
	}
}

func (d *driver) GetName() string {
	return string(clusterconfig.StorageTypeSwift)
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetName()},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_AUTHURL", Value: d.Config.AuthURL},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_SWIFT_CONTAINER", Value: d.Config.Container},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_SWIFT_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: d.Name + "-private-configuration",
					},
					Key: "REGISTRY_STORAGE_SWIFT_USERNAME",
				},
			},
		},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_SWIFT_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: d.Name + "-private-configuration",
					},
					Key: "REGISTRY_STORAGE_SWIFT_PASSWORD",
				},
			},
		},
	)
	return
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

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	return nil, nil, nil
}

func (d *driver) CompleteConfiguration(cr *opapi.ImageRegistry) error {
	return nil
}
