package s3

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	operatorapi "github.com/openshift/api/operator/v1"

	coreapi "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/uuid"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

var (
	s3Service *s3.S3
)

type driver struct {
	Config    *opapi.ImageRegistryConfigStorageS3
}

// getS3Service returns a client that allows us to interact
// with the aws S3 service
func (d *driver) getS3Service() (*s3.S3, error) {
	if s3Service != nil {
		return s3Service, nil
	}

	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		return nil, err
	}

	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(cfg.Storage.S3.AccessKey, cfg.Storage.S3.SecretKey, ""),
		Region:      &d.Config.Region,
	})
	if err != nil {
		return nil, err
	}

	s3Service := s3.New(sess)

	return s3Service, nil

}

// NewDriver creates a new s3 storage driver
// Used during bootstrapping
func NewDriver(c *opapi.ImageRegistryConfigStorageS3) *driver {
	return &driver{
		Config:    c,
	}
}

// UpdateFromStorage updates the driver from an image registry resource
func (d *driver) UpdateFromStorage(cfg opapi.ImageRegistryConfigStorage) {
	d.Config = cfg.S3.DeepCopy()
}

// GetType returns the type of the storage driver
func (d *driver) GetType() string {
	return string(clusterconfig.StorageTypeS3)
}

// ConfigEnv configures the environment variables that will be
// used in the image registry deployment
func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetType()},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_BUCKET", Value: d.Config.Bucket},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_REGION", Value: d.Config.Region},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_REGIONENDPOINT", Value: d.Config.RegionEndpoint},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_ENCRYPT", Value: fmt.Sprintf("%v", d.Config.Encrypt)},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_S3_ACCESSKEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: opapi.ImageRegistryPrivateConfiguration,
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
						Name: opapi.ImageRegistryPrivateConfiguration,
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

// SyncSecrets checks if the storage access secrets have been updated
// and returns a map of keys/data to update, or nil if they have not been
func (d *driver) SyncSecrets(sec *coreapi.Secret) (map[string]string, error) {
	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		return nil, err
	}

	// Get the existing SecretKey and AccessKey
	var existingAccessKey, existingSecretKey []byte
	if v, ok := sec.Data["REGISTRY_STORAGE_S3_ACCESSKEY"]; ok {
		existingAccessKey = v
	}
	if v, ok := sec.Data["REGISTRY_STORAGE_S3_SECRETKEY"]; ok {
		existingSecretKey = v
	}

	// Check if the existing SecretKey and AccessKey match what we got from the cluster or user configuration
	if !bytes.Equal([]byte(cfg.Storage.S3.AccessKey), existingAccessKey) || !bytes.Equal([]byte(cfg.Storage.S3.SecretKey), existingSecretKey) {

		data := map[string]string{
			"REGISTRY_STORAGE_S3_ACCESSKEY": cfg.Storage.S3.AccessKey,
			"REGISTRY_STORAGE_S3_SECRETKEY": cfg.Storage.S3.SecretKey,
		}

		return data, nil
	}

	return nil, nil
}

// bucketExists checks whether or not the s3 bucket exists
func (d *driver) bucketExists(bucketName string) (bool, error) {
	if len(bucketName) == 0 {
		return false, nil
	}

	svc, err := d.getS3Service()
	if err != nil {
		return false, err
	}

	_, err = svc.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return false, err
	}

	return true, nil
}

// getBucketLocation returns the region that the bucket exists in
func (d *driver) getBucketLocation(bucketName string) (string, error) {
	if len(bucketName) == 0 {
		return "", fmt.Errorf("the bucket name is blank")
	}

	svc, err := d.getS3Service()
	if err != nil {
		return "", err
	}
	getBucketLocationResult, err := svc.GetBucketLocation(&s3.GetBucketLocationInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return "", err
	}

	if *getBucketLocationResult.LocationConstraint == "" {
		*getBucketLocationResult.LocationConstraint = "us-east-1"
	} else if *getBucketLocationResult.LocationConstraint == "EU" {
		*getBucketLocationResult.LocationConstraint = "eu-west-1"
	}

	return *getBucketLocationResult.LocationConstraint, nil
}

// StorageExists checks if an S3 bucket with the given name exists
// and we can access it
func (d *driver) StorageExists(cr *opapi.ImageRegistry, modified *bool) (bool, error) {
	if len(d.Config.Bucket) == 0 {
		return false, nil
	}

	bucketExists, err := d.bucketExists(d.Config.Bucket)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket, "Forbidden", "NotFound":
				util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error(), modified)
				return false, nil
			default:
				util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error(), modified)
				return false, err
			}
		}
	}

	util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists", "", modified)
	return bucketExists, nil

}

// StorageChanged checks to see if the name of the storage medium
// has changed
func (d *driver) StorageChanged(cr *opapi.ImageRegistry, modified *bool) bool {
	if !reflect.DeepEqual(cr.Status.Storage.S3, cr.Spec.Storage.S3) {
		util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionUnknown, "S3 Configuration Changed", "S3 storage is in an unknown state", modified)
		return true
	}

	return false
}

// GetStorageName returns the current storage bucket that we are using
func (d *driver) GetStorageName() string {
	if d.Config == nil {
		return ""
	}
	return d.Config.Bucket
}

// CreateStorage attempts to create an s3 bucket
// and apply any provided tags
func (d *driver) CreateStorage(cr *opapi.ImageRegistry, modified *bool) error {
	svc, err := d.getS3Service()
	if err != nil {
		return err
	}

	ic, err := util.GetInstallConfig()
	if err != nil {
		return err
	}

	cv, err := util.GetClusterVersionConfig()
	if err != nil {
		return err
	}

	// If a bucket name is supplied, and it already exists and we can access it
	// just update the config
	var bucketExists bool
	if len(d.Config.Bucket) != 0 {
		bucketExists, err = d.bucketExists(d.Config.Bucket)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case s3.ErrCodeNoSuchBucket, "Forbidden", "NotFound":
					// If the bucket doesn't exist that's ok, we'll try to create it
					util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error(), modified)
				default:
					util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error(), modified)
					return err
				}
			}
		}
	}
	if len(d.Config.Bucket) != 0 && bucketExists {
		*cr.Status.Storage.S3 = *d.Config
		util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists", "User supplied S3 bucket exists and is accessible", modified)

	} else {
		generatedName := false
		for i := 0; i < 5000; i++ {
			// If the bucket name is blank, let's generate one
			if len(d.Config.Bucket) == 0 {
				d.Config.Bucket = fmt.Sprintf("%s-%s-%s-%s", clusterconfig.StoragePrefix, d.Config.Region, strings.Replace(string(cv.Spec.ClusterID), "-", "", -1), strings.Replace(string(uuid.NewUUID()), "-", "", -1))[0:62]
				generatedName = true
			}

			_, err := svc.CreateBucket(&s3.CreateBucketInput{
				Bucket: aws.String(d.Config.Bucket),
			})
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case s3.ErrCodeBucketAlreadyExists:
						if d.Config.Bucket != "" && !generatedName {
							util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, "Unable to Access Bucket", "The bucket exists, but we do not have permission to access it", modified)
							break
						}
						d.Config.Bucket = ""
						continue
					default:
						util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error(), modified)
						return err
					}
				}
			}
			cr.Status.StorageManaged = true
			cr.Status.Storage.S3 = d.Config.DeepCopy()
			cr.Spec.Storage.S3 = d.Config.DeepCopy()

			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionTrue, "Creation Successful", "S3 bucket was successfully created", modified)

			break
		}

		if len(d.Config.Bucket) == 0 {
			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, "Unable to Generate Unique Bucket Name", "", modified)
			return fmt.Errorf("unable to generate a unique s3 bucket name")
		}
	}

	// Wait until the bucket exists
	if err := svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}); err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error(), modified)
		}

		return err
	}

	// Tag the bucket with the openshiftClusterID
	// along with any user defined tags from the cluster configuration
	if ic.Platform.AWS != nil {
		var tagSet []*s3.Tag
		tagSet = append(tagSet, &s3.Tag{Key: aws.String("openshiftClusterID"), Value: aws.String(string(cv.Spec.ClusterID))})
		for k, v := range ic.Platform.AWS.UserTags {
			tagSet = append(tagSet, &s3.Tag{Key: aws.String(k), Value: aws.String(v)})
		}

		_, err := svc.PutBucketTagging(&s3.PutBucketTaggingInput{
			Bucket: aws.String(d.Config.Bucket),
			Tagging: &s3.Tagging{
				TagSet: tagSet,
			},
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				util.UpdateCondition(cr, opapi.StorageTagged, operatorapi.ConditionFalse, aerr.Code(), aerr.Error(), modified)
			} else {
				util.UpdateCondition(cr, opapi.StorageTagged, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error(), modified)
			}
		} else {
			util.UpdateCondition(cr, opapi.StorageTagged, operatorapi.ConditionTrue, "Tagging Successful", "UserTags were successfully applied to the S3 bucket", modified)
		}
	}

	// Enable default encryption on the bucket
	_, err = svc.PutBucketEncryption(&s3.PutBucketEncryptionInput{
		Bucket: aws.String(d.Config.Bucket),
		ServerSideEncryptionConfiguration: &s3.ServerSideEncryptionConfiguration{
			Rules: []*s3.ServerSideEncryptionRule{
				{
					ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
						SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256),
					},
				},
			},
		},
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, opapi.StorageEncrypted, operatorapi.ConditionFalse, aerr.Code(), aerr.Error(), modified)
		} else {
			util.UpdateCondition(cr, opapi.StorageEncrypted, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error(), modified)
		}
	} else {
		util.UpdateCondition(cr, opapi.StorageEncrypted, operatorapi.ConditionTrue, "Encryption Successful", "Default encryption was successfully enabled on the S3 bucket", modified)
	}

	// Enable default incomplete multipart upload cleanup after one (1) day
	_, err = svc.PutBucketLifecycleConfiguration(&s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(d.Config.Bucket),
		LifecycleConfiguration: &s3.BucketLifecycleConfiguration{
			Rules: []*s3.LifecycleRule{
				{
					ID:     aws.String("cleanup-incomplete-multipart-registry-uploads"),
					Status: aws.String("Enabled"),
					Filter: &s3.LifecycleRuleFilter{
						Prefix: aws.String(""),
					},
					AbortIncompleteMultipartUpload: &s3.AbortIncompleteMultipartUpload{
						DaysAfterInitiation: aws.Int64(1),
					},
				},
			},
		},
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, opapi.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionFalse, aerr.Code(), aerr.Error(), modified)
		} else {
			util.UpdateCondition(cr, opapi.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error(), modified)
		}
	} else {
		util.UpdateCondition(cr, opapi.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionTrue, "Enable Cleanup Successful", "Default cleanup of incomplete multipart uploads after one (1) day was successfully enabled", modified)
	}

	return nil
}

// RemoveStorage deletes the storage medium that we created
func (d *driver) RemoveStorage(cr *opapi.ImageRegistry, modified *bool) error {
	if !cr.Status.StorageManaged || len(d.Config.Bucket) == 0 {
		return nil
	}

	svc, err := d.getS3Service()
	if err != nil {
		return err
	}
	_, err = svc.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionUnknown, aerr.Code(), aerr.Error(), modified)
		}
		return err
	}

	cr.Spec.Storage.S3.Bucket = ""
	d.Config.Bucket = ""
	cr.Status.Storage.S3 = d.Config.DeepCopy()
	if d.StorageChanged(cr, modified) {
		*modified = true
	}

	util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, "S3 Bucket Deleted", "The S3 bucket has been removed.", modified)

	return nil

}

func (d *driver) CompleteConfiguration(cr *opapi.ImageRegistry, modified *bool) error {
	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		return err
	}

	if len(d.Config.Region) == 0 {
		d.Config.Region = cfg.Storage.S3.Region
	}
	if cr.Spec.Storage.S3 == nil {
		cr.Spec.Storage.S3 = &opapi.ImageRegistryConfigStorageS3{}
	}
	if cr.Status.Storage.S3 == nil {
		cr.Status.Storage.S3 = &opapi.ImageRegistryConfigStorageS3{}
	}
	cr.Spec.Storage.S3 = d.Config.DeepCopy()
	*modified = true

	return nil
}
