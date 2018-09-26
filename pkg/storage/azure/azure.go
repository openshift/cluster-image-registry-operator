package azure

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

type driver struct {
	Config *opapi.ImageRegistryConfigStorageAzure
}

func NewDriver(c *opapi.ImageRegistryConfigStorageAzure) *driver {
	return &driver{
		Config: c,
	}
}

func (d *driver) GetName() string {
	return "azure"
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetName()},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_AZURE_CONTAINER", Value: d.Config.Container},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_AZURE_ACCOUNTNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "image-registry-private-configuration",
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
						Name: "image-registry-private-configuration",
					},
					Key: "REGISTRY_STORAGE_AZURE_ACCOUNTKEY",
				},
			},
		},
	)

	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	return nil, nil, nil
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

	return nil
}
