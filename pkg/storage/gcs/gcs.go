package gcs

import (
	"context"
	"fmt"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/storage"

	"google.golang.org/api/googleapi"

	coreapi "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/uuid"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
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

func (d *driver) GetName() string {
	return "gcs"
}

func (d *driver) ConfigEnv() (envs []coreapi.EnvVar, err error) {
	envs = append(envs,
		coreapi.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetName()},
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

func (d *driver) createOrUpdatePrivateConfiguration(keyfileData string) error {
	data := make(map[string]string)

	data["STORAGE_GCS_KEYFILE"] = keyfileData

	return util.CreateOrUpdateSecret("image-registry", "openshift-image-registry", data)
}

func (d *driver) CompleteConfiguration() error {
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
			d.Config.Bucket = fmt.Sprintf("%s-%s", clusterconfig.STORAGE_PREFIX, string(uuid.NewUUID()))

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
		}
	}
	return d.createOrUpdatePrivateConfiguration(cfg.Storage.GCS.KeyfileData)
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

	if v, ok := util.GetStateValue(&cr.Status, "gcs-bucket"); ok {
		if v != d.Config.Bucket {
			return fmt.Errorf("GCS bucket change is not supported: expected bucket %s, but got %s", v, d.Config.Bucket)
		}
	} else {
		util.SetStateValue(&cr.Status, "gcs-bucket", d.Config.Bucket)
		*modified = true
	}

	return nil
}
