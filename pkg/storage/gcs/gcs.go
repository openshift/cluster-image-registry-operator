package gcs

import (
	"context"
	"fmt"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/storage"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
)

type driver struct {
	Config *opapi.OpenShiftDockerRegistryConfigStorageGCS
}

func NewDriver(c *opapi.OpenShiftDockerRegistryConfigStorageGCS) *driver {
	return &driver{
		Config: c,
	}
}

func (d *driver) GetName() string {
	return "gcs"
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetName()},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_GCS_BUCKET", Value: d.Config.Bucket},
	)
	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	return nil, nil, nil
}

func (d *driver) CompleteConfiguration() error {
	if len(d.Config.Bucket) == 0 {
		d.Config.Bucket = "image-registry"

		projectID, err := metadata.NewClient(nil).ProjectID()
		if err != nil {
			return err
		}

		ctx := context.Background()

		client, err := storage.NewClient(ctx)
		if err != nil {
			return err
		}

		err = client.Bucket(d.Config.Bucket).Create(ctx, projectID, nil)
		if err != nil {
			return err
		}
	}
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
			return fmt.Errorf("GCS bucket change is not supported: expected bucket %s, but got %s", v, d.Config.Bucket)
		}
	} else {
		prevState.Data["gcs-bucket"] = d.Config.Bucket
	}

	return nil
}
