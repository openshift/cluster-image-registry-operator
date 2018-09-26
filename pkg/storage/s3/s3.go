package s3

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

type driver struct {
	Config *opapi.ImageRegistryConfigStorageS3
}

func NewDriver(c *opapi.ImageRegistryConfigStorageS3) *driver {
	return &driver{
		Config: c,
	}
}

func (d *driver) GetName() string {
	return "s3"
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetName()},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_BUCKET", Value: d.Config.Bucket},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_REGION", Value: d.Config.Region},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_REGIONENDPOINT", Value: d.Config.RegionEndpoint},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_ENCRYPT", Value: fmt.Sprintf("%v", d.Config.Encrypt)},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_S3_ACCESSKEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "image-registry-private-configuration",
					},
					Key: "REGISTRY_STORAGE_S3_ACCESSKEY",
				},
			},
		},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_S3_SECRETKEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "image-registry-private-configuration",
					},
					Key: "REGISTRY_STORAGE_S3_SECRETKEY",
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

	if v, ok := prevState.Data["gcs-bucket"]; ok {
		if v != d.Config.Bucket {
			return fmt.Errorf("S3 bucket change is not supported: expected bucket %s, but got %s", v, d.Config.Bucket)
		}
	} else {
		prevState.Data["s3-bucket"] = d.Config.Bucket
	}

	if v, ok := prevState.Data["s3-region"]; ok {
		if v != d.Config.Region {
			return fmt.Errorf("S3 region change is not supported: expected region %s, but got %s", v, d.Config.Region)
		}
	} else {
		prevState.Data["s3-region"] = d.Config.Region
	}

	return nil
}
