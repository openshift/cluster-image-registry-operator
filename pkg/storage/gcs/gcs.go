package gcs

import (
	"context"
	"fmt"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/storage"

	"google.golang.org/api/googleapi"

	coreapi "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/uuid"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
)

type driver struct {
	Config  *imageregistryv1.ImageRegistryConfigStorageGCS
	Listers *regopclient.Listers
}

func NewDriver(c *imageregistryv1.ImageRegistryConfigStorageGCS, listers *regopclient.Listers) *driver {
	return &driver{
		Config:  c,
		Listers: listers,
	}
}

// Secrets returns a storage access secrets.
func (d *driver) Secrets() (map[string]string, error) {
	cfg, err := clusterconfig.GetGCSConfig()
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"STORAGE_GCS_KEYFILE": cfg.Storage.GCS.KeyfileData,
	}, nil
}

func (d *driver) ConfigEnv() (envs []coreapi.EnvVar, err error) {
	envs = append(envs,
		coreapi.EnvVar{Name: "REGISTRY_STORAGE", Value: "gcs"},
		coreapi.EnvVar{Name: "REGISTRY_STORAGE_GCS_BUCKET", Value: d.Config.Bucket},
		coreapi.EnvVar{Name: "REGISTRY_STORAGE_GCS_KEYFILE", Value: "/gcs/keyfile"},
	)
	return
}

func (d *driver) Volumes() ([]coreapi.Volume, []coreapi.VolumeMount, error) {
	vol := coreapi.Volume{
		Name: "registry-storage-keyfile",
		VolumeSource: coreapi.VolumeSource{
			Projected: &coreapi.ProjectedVolumeSource{
				Sources: []coreapi.VolumeProjection{
					{
						Secret: &coreapi.SecretProjection{
							LocalObjectReference: coreapi.LocalObjectReference{
								Name: imageregistryv1.ImageRegistryPrivateConfiguration,
							},
							Items: []coreapi.KeyToPath{
								{
									Key:  "STORAGE_GCS_KEYFILE",
									Path: "keyfile",
								},
							},
						},
					},
				},
			},
		},
	}

	mount := coreapi.VolumeMount{
		Name:      vol.Name,
		MountPath: "/gcs",
	}

	return []coreapi.Volume{vol}, []coreapi.VolumeMount{mount}, nil
}

func (d *driver) StorageExists(cr *imageregistryv1.Config, modified *bool) (bool, error) {
	return false, nil
}

func (d *driver) StorageChanged(cr *imageregistryv1.Config, modified *bool) bool {
	return false
}

func (d *driver) CreateStorage(cr *imageregistryv1.Config, modified *bool) error {
	return nil
}

func (d *driver) RemoveStorage(cr *imageregistryv1.Config, modified *bool) (bool, error) {
	if !cr.Status.StorageManaged {
		return false, nil
	}

	return false, nil
}

func (d *driver) CompleteConfiguration(cr *imageregistryv1.Config) error {
	// Apply global config
	cfg, err := clusterconfig.GetGCSConfig()
	if err != nil {
		return err
	}

	if len(d.Config.Bucket) == 0 {
		d.Config.Bucket = cfg.Storage.GCS.Bucket
	}

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

		for {
			d.Config.Bucket = fmt.Sprintf("%s-%s", imageregistryv1.ImageRegistryName, string(uuid.NewUUID()))

			err = client.Bucket(d.Config.Bucket).Create(ctx, projectID, nil)

			switch e := err.(type) {
			case nil:
				cr.Status.StorageManaged = true
				break
			case *googleapi.Error:
				// Code 429 has already been processed.
				if e.Code >= 400 && e.Code < 500 {
					return err
				}
			}
		}

	}

	cr.Status.Storage.GCS = d.Config.DeepCopy()

	return nil

}
