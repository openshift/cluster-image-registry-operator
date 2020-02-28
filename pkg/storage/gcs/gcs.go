package gcs

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"

	gstorage "cloud.google.com/go/storage"
	goauth2 "golang.org/x/oauth2/google"
	gapi "google.golang.org/api/googleapi"
	goption "google.golang.org/api/option"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

type GCS struct {
	KeyfileData string
	Region      string
	ProjectID   string
}

type driver struct {
	Context    context.Context
	Config     *imageregistryv1.ImageRegistryConfigStorageGCS
	KubeConfig *rest.Config
	Listers    *regopclient.Listers
}

func NewDriver(ctx context.Context, c *imageregistryv1.ImageRegistryConfigStorageGCS, kubeconfig *rest.Config, listers *regopclient.Listers) *driver {
	return &driver{
		Context:    ctx,
		Config:     c,
		KubeConfig: kubeconfig,
		Listers:    listers,
	}
}

// getGCSClient returns a client that allows us to interact
// with the GCS services
func (d *driver) getGCSClient() (*gstorage.Client, error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	if len(d.Config.Region) == 0 {
		d.Config.Region = cfg.Region
		d.Config.ProjectID = cfg.ProjectID
	}

	credentials, err := goauth2.CredentialsFromJSON(d.Context, []byte(cfg.KeyfileData), gstorage.ScopeFullControl)
	if err != nil {
		return nil, err
	}

	gcsClient, err := gstorage.NewClient(d.Context, goption.WithCredentials(credentials))
	if err != nil {
		return nil, err
	}

	return gcsClient, nil
}

// GetConfig reads configuration for the GCS cloud platform services.
func GetConfig(listers *regopclient.Listers) (*GCS, error) {
	gcsConfig := &GCS{}

	infra, err := listers.Infrastructures.Get("cluster")
	if err != nil {
		return nil, err
	}

	if infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.Type == configapiv1.GCPPlatformType {
		gcsConfig.Region = infra.Status.PlatformStatus.GCP.Region
		gcsConfig.ProjectID = infra.Status.PlatformStatus.GCP.ProjectID
	}

	// Look for a user defined secret to get the AWS credentials from first
	sec, err := listers.Secrets.Get(defaults.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
		// Fall back to those provided by the credential minter if nothing is provided by the user
		sec, err = listers.Secrets.Get(defaults.CloudCredentialsName)
		if err != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials %q: %v", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.CloudCredentialsName), err)
		}
		if v, ok := sec.Data["service_account.json"]; ok {
			gcsConfig.KeyfileData = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"service_account.json\"", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.CloudCredentialsName))
		}
	} else if err != nil {
		return nil, err
	} else {
		if v, ok := sec.Data["REGISTRY_STORAGE_GCS_KEYFILE"]; ok {
			gcsConfig.KeyfileData = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_GCS_KEYFILE\"", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser))
		}
	}

	return gcsConfig, nil
}

func (d *driver) Secrets() (map[string]string, error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"REGISTRY_STORAGE_GCS_KEYFILE": cfg.KeyfileData,
	}, nil
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: "gcs"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_GCS_BUCKET", Value: d.Config.Bucket},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_GCS_KEYFILE", Value: "/gcs/keyfile"},
	)
	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	optional := false

	vol := corev1.Volume{
		Name: "registry-storage-keyfile",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: defaults.ImageRegistryPrivateConfiguration,
				Items: []corev1.KeyToPath{
					{
						Key:  "REGISTRY_STORAGE_GCS_KEYFILE",
						Path: "keyfile",
					},
				},
				Optional: &optional,
			},
		},
	}

	mount := corev1.VolumeMount{
		Name:      vol.Name,
		MountPath: "/gcs",
	}

	return []corev1.Volume{vol}, []corev1.VolumeMount{mount}, nil
}

func (d *driver) bucketExists(bucketName string) error {
	client, err := d.getGCSClient()
	if err != nil {
		return err
	}

	_, err = client.Bucket(d.Config.Bucket).Attrs(d.Context)

	return err
}

func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	if len(d.Config.Bucket) == 0 {
		return false, nil
	}

	err := d.bucketExists(d.Config.Bucket)
	if err != nil && err == gstorage.ErrBucketNotExist {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Bucket does not exist", err.Error())
		return false, nil
	} else if err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
		return false, err
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "GCS Bucket Exists", "")

	return true, nil
}

func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.GCS, cr.Spec.Storage.GCS) {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "GCS Configuration Changed", "GCS storage is in an unknown state")
		return true
	}

	return false
}

func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	gclient, err := d.getGCSClient()
	if err != nil {
		return err
	}

	// If a bucket name is supplied, and it already exists and we can access it
	// just update the config
	var bucket *gstorage.BucketHandle
	var bucketExists bool
	if len(d.Config.Bucket) != 0 {
		err = d.bucketExists(d.Config.Bucket)
		if err != nil {
			if gerr, ok := err.(*gapi.Error); ok {
				switch err {
				case gstorage.ErrBucketNotExist:
					// If the bucket doesn't exist that's ok, we'll try to create it
					util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, strconv.Itoa(gerr.Code), gerr.Error())
				default:
					util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
					return err
				}
			} else {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
				return err
			}
		} else {
			bucketExists = true
		}

	}
	if len(d.Config.Bucket) != 0 && bucketExists {
		bucket = gclient.Bucket(d.Config.Bucket)
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			GCS: d.Config.DeepCopy(),
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "GCS Bucket Exists", "User supplied GCS bucket exists and is accessible")

	} else {
		const numRetries = 5000
		for i := 0; i < numRetries; i++ {
			// If the bucket name is blank, let's generate one
			if len(d.Config.Bucket) == 0 {
				if d.Config.Bucket, err = util.GenerateStorageName(d.Listers, d.Config.Region); err != nil {
					return err
				}
			}
			bucketAttrs := gstorage.BucketAttrs{Location: d.Config.Region}
			bucket = gclient.Bucket(d.Config.Bucket)

			err := bucket.Create(d.Context, d.Config.ProjectID, &bucketAttrs)
			if err != nil {
				if gerr, ok := err.(*gapi.Error); ok {
					util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, strconv.Itoa(gerr.Code), gerr.Error())
					return err
				} else {
					util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
					return err
				}
			}
			cr.Status.StorageManaged = true
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				GCS: d.Config.DeepCopy(),
			}
			cr.Spec.Storage.GCS = d.Config.DeepCopy()

			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Creation Successful", "GCS bucket was successfully created")

			break
		}

		if len(d.Config.Bucket) == 0 {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Unable to Generate Unique Bucket Name", "")
			return fmt.Errorf("unable to generate a unique GCS bucket name")
		}
	}

	// TODO: Wait until the bucket exists

	// Set KMS Key ID for encryption on the bucket (if specified)
	// Data is encrypted by default on GCS: https://cloud.google.com/storage/docs/encryption/
	if cr.Status.StorageManaged {
		if len(d.Config.KeyID) != 0 {
			_, err := bucket.Update(d.Context, gstorage.BucketAttrsToUpdate{
				Encryption: &gstorage.BucketEncryption{
					DefaultKMSKeyName: d.Config.KeyID,
				},
			})

			if err != nil {
				if gerr, ok := err.(*gapi.Error); ok {
					util.UpdateCondition(cr, defaults.StorageEncrypted, operatorapi.ConditionFalse, "InvalidStorageConfiguration", gerr.Error())
					return err
				} else {
					util.UpdateCondition(cr, defaults.StorageEncrypted, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
				}
			} else {
				util.UpdateCondition(cr, defaults.StorageEncrypted, operatorapi.ConditionTrue, "Encryption Successful", "KMS encryption was successfully enabled on the GCS bucket")
				cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
					GCS: d.Config.DeepCopy(),
				}
				cr.Spec.Storage.GCS = d.Config.DeepCopy()
			}
		}
	} else {
		if !reflect.DeepEqual(cr.Status.Storage.GCS, d.Config) {
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				GCS: d.Config.DeepCopy(),
			}
		}
	}

	return nil
}

func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (bool, error) {
	if !cr.Status.StorageManaged {
		return false, nil
	}
	gclient, err := d.getGCSClient()
	if err != nil {
		return false, err
	}

	if err = gclient.Bucket(d.Config.Bucket).Delete(d.Context); err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "", err.Error())
		return false, err
	}

	if len(cr.Spec.Storage.GCS.Bucket) != 0 {
		cr.Spec.Storage.GCS.Bucket = ""
	}

	d.Config.Bucket = ""

	if !reflect.DeepEqual(cr.Status.Storage.GCS, d.Config) {
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			GCS: d.Config.DeepCopy(),
		}
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "GCS Bucket Deleted", "The GCS bucket has been removed.")

	return true, nil
}

// ID return the underlying storage identificator, on this case the bucket name.
func (d *driver) ID() string {
	return d.Config.Bucket
}
