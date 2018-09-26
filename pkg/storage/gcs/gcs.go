package gcs

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

type driver struct {
	Config *opapi.ImageRegistryConfigStorageGCS
}

func NewDriver(c *opapi.ImageRegistryConfigStorageGCS) *driver {
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
		projectID, err := metadata.NewClient(nil).ProjectID()
		if err != nil {
			return err
		}

		ctx := context.Background()

		client, err := storage.NewClient(ctx)
		if err != nil {
			return err
		}

		d.Config.Bucket = fmt.Sprintf("image-registry-%d", time.Now().UnixNano())

		for {
			err = client.Bucket(d.Config.Bucket).Create(ctx, projectID, nil)

			switch e := err.(type) {
			case nil:
				break
			case *googleapi.Error:
				// Code 429 has already been processed.
				if e.Code >= 400 && e.Code < 500 {
					return err
				}
			}

			d.Config.Bucket = fmt.Sprintf("image-registry-%d", time.Now().UnixNano())
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
