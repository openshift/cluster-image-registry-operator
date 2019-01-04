package gcs

import (
	"bytes"
	"context"
	"fmt"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/storage"

	"google.golang.org/api/googleapi"

	coreapi "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/uuid"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
)

type driver struct {
	Name      string
	Namespace string
	Config    *opapi.ImageRegistryConfigStorageGCS
}

func NewDriver(crname string, crnamespace string, c *opapi.ImageRegistryConfigStorageGCS) *driver {
	return &driver{
		Name:      crname,
		Namespace: crnamespace,
		Config:    c,
	}
}

func (d *driver) GetType() string {
	return string(clusterconfig.StorageTypeGCS)
}

// SyncSecrets checks if the storage access secrets have been updated
// and returns a map of keys/data to update, or nil if they have not been
func (d *driver) SyncSecrets(sec *coreapi.Secret) (map[string]string, error) {
	cfg, err := clusterconfig.GetGCSConfig()
	if err != nil {
		return nil, err
	}

	// Get the existing KeyFileData
	var existingKeyfileData []byte
	if v, ok := sec.Data["STORAGE_GCS_KEYFILE"]; ok {
		existingKeyfileData = v
	}

	// Check if the existing SecretKey and AccessKey match what we got from the cluster or user configuration
	if !bytes.Equal([]byte(cfg.Storage.GCS.KeyfileData), existingKeyfileData) {

		data := map[string]string{
			"STORAGE_GCS_KEYFILE": cfg.Storage.GCS.KeyfileData,
		}
		return data, nil

	}
	return nil, nil
}

func (d *driver) ConfigEnv() (envs []coreapi.EnvVar, err error) {
	envs = append(envs,
		coreapi.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetType()},
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
								Name: d.Name + "-private-configuration",
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

func (d *driver) StorageExists(cr *opapi.ImageRegistry, modified *bool) (bool, error) {
	return false, nil
}

func (d *driver) StorageChanged(cr *opapi.ImageRegistry, modified *bool) bool {
	return false
}

func (d *driver) GetStorageName(cr *opapi.ImageRegistry, modified *bool) (string, error) {
	if cr.Spec.Storage.GCS != nil {
		return cr.Spec.Storage.GCS.Bucket, nil
	}
	return "", fmt.Errorf("unable to retrieve bucket name from image registry resource: %#v", cr.Spec.Storage)
}

func (d *driver) CreateStorage(cr *opapi.ImageRegistry, modified *bool) error {
	return nil
}

func (d *driver) RemoveStorage(cr *opapi.ImageRegistry, modified *bool) error {
	if !cr.Status.Storage.Managed {
		return nil
	}

	return nil
}

func (d *driver) CompleteConfiguration(cr *opapi.ImageRegistry, modified *bool) error {
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
			d.Config.Bucket = fmt.Sprintf("%s-%s", clusterconfig.StoragePrefix, string(uuid.NewUUID()))

			err = client.Bucket(d.Config.Bucket).Create(ctx, projectID, nil)

			switch e := err.(type) {
			case nil:
				cr.Status.Storage.Managed = true
				break
			case *googleapi.Error:
				// Code 429 has already been processed.
				if e.Code >= 400 && e.Code < 500 {
					return err
				}
			}
		}

	}

	cr.Status.Storage.State.GCS = d.Config
	*modified = true

	return nil

}
