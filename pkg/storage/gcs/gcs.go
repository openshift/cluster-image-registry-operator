package gcs

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"

	gstorage "cloud.google.com/go/storage"
	goauth2 "golang.org/x/oauth2/google"
	gapi "google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	goption "google.golang.org/api/option"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

type GCS struct {
	KeyfileData string
	Region      string
	ProjectID   string
}

type driver struct {
	Context context.Context
	Config  *imageregistryv1.ImageRegistryConfigStorageGCS
	Listers *regopclient.StorageListers

	// httpClient is used only during tests.
	httpClient *http.Client
}

func NewDriver(ctx context.Context, c *imageregistryv1.ImageRegistryConfigStorageGCS, listers *regopclient.StorageListers) *driver {
	return &driver{
		Context: ctx,
		Config:  c,
		Listers: listers,
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

	opts := []goption.ClientOption{goption.WithCredentials(credentials)}
	if d.httpClient != nil {
		opts = append(opts, goption.WithHTTPClient(d.httpClient))
	}

	gcsClient, err := gstorage.NewClient(d.Context, opts...)
	if err != nil {
		return nil, err
	}

	return gcsClient, nil
}

// GetConfig reads configuration for the GCS cloud platform services.
func GetConfig(listers *regopclient.StorageListers) (*GCS, error) {
	gcsConfig := &GCS{}

	infra, err := util.GetInfrastructure(listers)
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

func (d *driver) CABundle() (string, bool, error) {
	return "", true, nil
}

func (d *driver) ConfigEnv() (envs envvar.List, err error) {
	envs = append(envs,
		envvar.EnvVar{Name: "REGISTRY_STORAGE", Value: "gcs"},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_GCS_BUCKET", Value: d.Config.Bucket},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_GCS_KEYFILE", Value: "/gcs/keyfile"},
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

func (d *driver) VolumeSecrets() (map[string]string, error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"REGISTRY_STORAGE_GCS_KEYFILE": cfg.KeyfileData,
	}, nil
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
	var bucketCreated bool
	if len(d.Config.Bucket) != 0 {
		if err := d.bucketExists(d.Config.Bucket); err == nil {
			bucketExists = true
		} else if err != gstorage.ErrBucketNotExist {
			util.UpdateCondition(
				cr,
				defaults.StorageExists,
				operatorapi.ConditionUnknown,
				"Unknown Error Occurred",
				err.Error(),
			)
			return err
		}
	}
	if len(d.Config.Bucket) != 0 && bucketExists {
		bucket = gclient.Bucket(d.Config.Bucket)
		if cr.Spec.Storage.ManagementState == "" {
			cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateUnmanaged
		}
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			GCS: d.Config.DeepCopy(),
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "GCS Bucket Exists", "User supplied GCS bucket exists and is accessible")
	} else {
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
		if cr.Spec.Storage.ManagementState == "" {
			cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateManaged
		}
		bucketCreated = true
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			GCS: d.Config.DeepCopy(),
		}
		cr.Spec.Storage.GCS = d.Config.DeepCopy()

		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Creation Successful", "GCS bucket was successfully created")
	}

	// TODO: Wait until the bucket exists

	// Set KMS Key ID for encryption on the bucket (if specified)
	// Data is encrypted by default on GCS: https://cloud.google.com/storage/docs/encryption/
	if bucketCreated {
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
	if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged {
		return false, nil
	}
	gclient, err := d.getGCSClient()
	if err != nil {
		return false, err
	}

	itr := gclient.Bucket(d.Config.Bucket).Objects(d.Context, nil)
	klog.V(5).Infof("deleting all objects in bucket %s", d.Config.Bucket)
	for attr, err := itr.Next(); err == nil || err != iterator.Done; {
		if err != nil {
			return false, err
		}
		klog.V(5).Infof("deleting object %s", attr.Name)
		deleteErr := gclient.Bucket(attr.Bucket).Object(attr.Name).Delete(d.Context)
		if deleteErr != nil {
			return false, deleteErr
		}
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
