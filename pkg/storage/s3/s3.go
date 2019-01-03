package s3

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	operatorapi "github.com/openshift/api/operator/v1alpha1"

	coreapi "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/uuid"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

var (
	s3Service *s3.S3
)

type driver struct {
	Name      string
	Namespace string
	Config    *opapi.ImageRegistryConfigStorageS3
}

// getSVC returns a service client that allows us to interact
// with the aws S3 service
func (d *driver) getSVC() (*s3.S3, error) {
	if s3Service != nil {
		return s3Service, nil
	}

	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		return nil, err
	}

	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(cfg.Storage.S3.AccessKey, cfg.Storage.S3.SecretKey, ""),
		Region:      &cfg.Storage.S3.Region,
	})
	if err != nil {
		return nil, err
	}
	s3Service := s3.New(sess)

	return s3Service, nil

}

func NewDriver(crname string, crnamespace string, c *opapi.ImageRegistryConfigStorageS3) *driver {
	return &driver{
		Name:      crname,
		Namespace: crnamespace,
		Config:    c,
	}
}

// GetType returns the name of the storage driver
func (d *driver) GetType() string {
	return string(clusterconfig.StorageTypeS3)
}

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
						Name: d.Name + "-private-configuration",
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
						Name: d.Name + "-private-configuration",
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

// StorageExists checks if an S3 bucket with the given name exists
// and we can access it
func (d *driver) StorageExists(cr *opapi.ImageRegistry) (bool, error) {
	if d.Config.Bucket == "" {
		return false, nil
	}

	svc, err := d.getSVC()
	if err != nil {
		return false, err
	}
	_, err = svc.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket, "Forbidden", "NotFound":
				util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
				return false, nil
			default:
				util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
				return false, err
			}
		}
	}

	util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists", "")
	return true, nil

}

// StorageChanged checks to see if the name of the storage medium
// has changed
func (d *driver) StorageChanged(cr *opapi.ImageRegistry) bool {
	var storageChanged bool

	if cr.Status.Storage.State.S3 == nil || cr.Status.Storage.State.S3.Bucket != cr.Spec.Storage.S3.Bucket {
		storageChanged = true
	}

	util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionUnknown, "S3 Bucket Changed", "S3 bucket is in an unknown state")

	return storageChanged
}

// GetStorageName returns the current storage bucket that we are using
func (d *driver) GetStorageName(cr *opapi.ImageRegistry) (string, error) {
	if cr.Spec.Storage.S3 != nil {
		return cr.Spec.Storage.S3.Bucket, nil
	}
	return "", fmt.Errorf("unable to retrieve bucket name from image registry resource: %#v", cr.Spec.Storage)
}

// CreateStorage attempts to create an s3 bucket
// and apply any provided tags
func (d *driver) CreateStorage(cr *opapi.ImageRegistry) error {
	svc, err := d.getSVC()
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

	for i := 0; i < 5000; i++ {
		if len(d.Config.Bucket) == 0 {
			d.Config.Bucket = fmt.Sprintf("%s-%s-%s-%s", clusterconfig.StoragePrefix, d.Config.Region, strings.Replace(string(cv.Spec.ClusterID), "-", "", -1), strings.Replace(string(uuid.NewUUID()), "-", "", -1))[0:62]
		}

		_, err := svc.CreateBucket(&s3.CreateBucketInput{
			Bucket: aws.String(d.Config.Bucket),
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case s3.ErrCodeBucketAlreadyExists:
					if cr.Spec.Storage.S3.Bucket != "" {
						break
					}
					d.Config.Bucket = ""
					continue
				default:
					util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
					return err
				}
			}
		}
		break
	}

	if len(cr.Spec.Storage.S3.Bucket) == 0 && len(d.Config.Bucket) == 0 {
		util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, "Unable to Generate Unique Bucket Name", "")
		return fmt.Errorf("unable to generate a unique s3 bucket name")
	}

	// Wait until the bucket exists
	if err := svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}); err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
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
				util.UpdateCondition(cr, opapi.StorageTagged, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			} else {
				util.UpdateCondition(cr, opapi.StorageTagged, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, opapi.StorageTagged, operatorapi.ConditionTrue, "Tagging Successful", "UserTags were successfully applied to the S3 bucket")
		}
	}

	// Enable default encryption on the bucket
	_, err = svc.PutBucketEncryption(&s3.PutBucketEncryptionInput{
		Bucket: aws.String(d.Config.Bucket),
		ServerSideEncryptionConfiguration: &s3.ServerSideEncryptionConfiguration{
			Rules: []*s3.ServerSideEncryptionRule{
				&s3.ServerSideEncryptionRule{
					ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
						SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256),
					},
				},
			},
		},
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {

			util.UpdateCondition(cr, opapi.StorageEncrypted, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
		} else {
			util.UpdateCondition(cr, opapi.StorageEncrypted, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
		}
	} else {
		util.UpdateCondition(cr, opapi.StorageEncrypted, operatorapi.ConditionTrue, "Encryption Successful", "Default encryption was successfully enabled on the S3 bucket")
	}

	cr.Status.Storage.State.S3 = d.Config
	cr.Status.Storage.Managed = true

	util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionTrue, "Creation Successful", "S3 bucket was successfully created")

	return nil
}

// RemoveStorage deletes the storage medium that we created
func (d *driver) RemoveStorage(cr *opapi.ImageRegistry) error {
	if !cr.Status.Storage.Managed {
		return nil
	}

	svc, err := d.getSVC()
	if err != nil {
		return err
	}
	_, err = svc.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionUnknown, aerr.Code(), aerr.Error())
		}
	}

	util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, "S3 Bucket Deleted", "The S3 bucket has been removed.")

	return err

}

func (d *driver) CompleteConfiguration(cr *opapi.ImageRegistry) error {
	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		return err
	}

	if len(d.Config.Bucket) == 0 {
		d.Config.Bucket = cfg.Storage.S3.Bucket
	}

	if len(d.Config.Region) == 0 {
		d.Config.Region = cfg.Storage.S3.Region
	}

	cr.Status.Storage.State.S3 = d.Config

	return nil
}
