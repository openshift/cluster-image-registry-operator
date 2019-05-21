package s3

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	operatorapi "github.com/openshift/api/operator/v1"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/uuid"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	"github.com/openshift/cluster-image-registry-operator/version"
)

var (
	s3Service *s3.S3
)

type driver struct {
	Config  *imageregistryv1.ImageRegistryConfigStorageS3
	Listers *regopclient.Listers
}

// NewDriver creates a new s3 storage driver
// Used during bootstrapping
func NewDriver(c *imageregistryv1.ImageRegistryConfigStorageS3, listers *regopclient.Listers) *driver {
	return &driver{
		Config:  c,
		Listers: listers,
	}
}

// getS3Service returns a client that allows us to interact
// with the aws S3 service
func (d *driver) getS3Service() (*s3.S3, error) {
	if s3Service != nil {
		return s3Service, nil
	}

	cfg, err := clusterconfig.GetAWSConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	if len(d.Config.Region) == 0 {
		d.Config.Region = cfg.Storage.S3.Region
	}

	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(cfg.Storage.S3.AccessKey, cfg.Storage.S3.SecretKey, ""),
		Region:      &d.Config.Region,
		Endpoint:    &d.Config.RegionEndpoint,
	})
	if err != nil {
		return nil, err
	}
	sess.Handlers.Build.PushBackNamed(request.NamedHandler{
		Name: "openshift.io/cluster-image-registry-operator",
		Fn:   request.MakeAddToUserAgentHandler("openshift.io cluster-image-registry-operator", version.Version),
	})

	s3Service := s3.New(sess)

	return s3Service, nil

}

// ConfigEnv configures the environment variables that will be
// used in the image registry deployment
func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	if len(d.Config.RegionEndpoint) != 0 {
		envs = append(envs, corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_REGIONENDPOINT", Value: d.Config.RegionEndpoint})
	}

	if len(d.Config.KeyID) != 0 {
		envs = append(envs, corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_KEYID", Value: d.Config.KeyID})
	}

	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: "s3"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_BUCKET", Value: d.Config.Bucket},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_REGION", Value: d.Config.Region},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_S3_ENCRYPT", Value: fmt.Sprintf("%v", d.Config.Encrypt)},
		corev1.EnvVar{
			Name: "REGISTRY_STORAGE_S3_ACCESSKEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: imageregistryv1.ImageRegistryPrivateConfiguration,
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
						Name: imageregistryv1.ImageRegistryPrivateConfiguration,
					},
					Key: "REGISTRY_STORAGE_S3_SECRETKEY",
				},
			},
		},
	)

	if d.Config.CloudFront != nil {
		envs = append(envs,
			corev1.EnvVar{Name: "REGISTRY_MIDDLEWARE_STORAGE_CLOUDFRONT_BASEURL", Value: d.Config.CloudFront.BaseURL},
			corev1.EnvVar{Name: "REGISTRY_MIDDLEWARE_STORAGE_CLOUDFRONT_KEYPAIRID", Value: d.Config.CloudFront.KeypairID},
			corev1.EnvVar{Name: "REGISTRY_MIDDLEWARE_STORAGE_CLOUDFRONT_DURATION", Value: d.Config.CloudFront.Duration.String()},
			corev1.EnvVar{Name: "REGISTRY_MIDDLEWARE_STORAGE_CLOUDFRONT_PRIVATEKEY", Value: "/etc/docker/cloudfront/private.pem"},
		)
	}

	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	if d.Config.CloudFront == nil {
		return nil, nil, nil
	}

	optional := false

	vol := corev1.Volume{
		Name: "registry-cloudfront",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: d.Config.CloudFront.PrivateKey.Name,
				Items: []corev1.KeyToPath{
					{Key: d.Config.CloudFront.PrivateKey.Key, Path: "private.pem"},
				},
				Optional: &optional,
			},
		},
	}

	mount := corev1.VolumeMount{
		Name:      vol.Name,
		MountPath: "/etc/docker/cloudfront",
		ReadOnly:  true,
	}

	return []corev1.Volume{vol}, []corev1.VolumeMount{mount}, nil
}

// Secrets returns a map of the storage access secrets.
func (d *driver) Secrets() (map[string]string, error) {
	cfg, err := clusterconfig.GetAWSConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"REGISTRY_STORAGE_S3_ACCESSKEY": cfg.Storage.S3.AccessKey,
		"REGISTRY_STORAGE_S3_SECRETKEY": cfg.Storage.S3.SecretKey,
	}, nil
}

// bucketExists checks whether or not the s3 bucket exists
func (d *driver) bucketExists(bucketName string) error {
	if len(bucketName) == 0 {
		return nil
	}

	svc, err := d.getS3Service()
	if err != nil {
		return err
	}

	_, err = svc.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})

	return err
}

// StorageExists checks if an S3 bucket with the given name exists
// and we can access it
func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	if len(d.Config.Bucket) == 0 {
		return false, nil
	}

	err := d.bucketExists(d.Config.Bucket)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket, "Forbidden", "NotFound":
				util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
				return false, nil
			}
		}
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
		return false, err
	}

	util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists", "")
	return true, nil

}

// StorageChanged checks to see if the name of the storage medium
// has changed
func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.S3, cr.Spec.Storage.S3) {
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionUnknown, "S3 Configuration Changed", "S3 storage is in an unknown state")
		return true
	}

	return false
}

// CreateStorage attempts to create an s3 bucket
// and apply any provided tags
func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	svc, err := d.getS3Service()
	if err != nil {
		return err
	}

	ic, err := clusterconfig.GetInstallConfig()
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
		err = d.bucketExists(d.Config.Bucket)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case s3.ErrCodeNoSuchBucket, "Forbidden", "NotFound":
					// If the bucket doesn't exist that's ok, we'll try to create it
					util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
				default:
					util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
					return err
				}
			} else {
				util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
				return err
			}
		} else {
			bucketExists = true
		}

	}
	if len(d.Config.Bucket) != 0 && bucketExists {
		cr.Status.Storage.S3 = d.Config.DeepCopy()
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists", "User supplied S3 bucket exists and is accessible")

	} else {
		generatedName := false
		for i := 0; i < 5000; i++ {
			// If the bucket name is blank, let's generate one
			if len(d.Config.Bucket) == 0 {
				d.Config.Bucket = fmt.Sprintf("%s-%s-%s-%s", imageregistryv1.ImageRegistryName, d.Config.Region, strings.Replace(string(cv.Spec.ClusterID), "-", "", -1), strings.Replace(string(uuid.NewUUID()), "-", "", -1))[0:62]
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
							util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, "Unable to Access Bucket", "The bucket exists, but we do not have permission to access it")
							break
						}
						d.Config.Bucket = ""
						continue
					default:
						util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
						return err
					}
				}
			}
			cr.Status.StorageManaged = true
			cr.Status.Storage.S3 = d.Config.DeepCopy()
			cr.Spec.Storage.S3 = d.Config.DeepCopy()

			util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionTrue, "Creation Successful", "S3 bucket was successfully created")

			break
		}

		if len(d.Config.Bucket) == 0 {
			util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, "Unable to Generate Unique Bucket Name", "")
			return fmt.Errorf("unable to generate a unique s3 bucket name")
		}
	}

	// Wait until the bucket exists
	if err := svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}); err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
		}

		return err
	}

	// Tag the bucket with the openshiftClusterID
	// along with any user defined tags from the cluster configuration
	if cr.Status.StorageManaged {
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
					util.UpdateCondition(cr, imageregistryv1.StorageTagged, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
				} else {
					util.UpdateCondition(cr, imageregistryv1.StorageTagged, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
				}
			} else {
				util.UpdateCondition(cr, imageregistryv1.StorageTagged, operatorapi.ConditionTrue, "Tagging Successful", "UserTags were successfully applied to the S3 bucket")
			}
		}
	}

	// Enable default encryption on the bucket
	if cr.Status.StorageManaged {
		var encryption *s3.ServerSideEncryptionByDefault
		var encryptionType string

		if len(d.Config.KeyID) != 0 {
			encryption = &s3.ServerSideEncryptionByDefault{
				SSEAlgorithm:   aws.String(s3.ServerSideEncryptionAwsKms),
				KMSMasterKeyID: aws.String(d.Config.KeyID),
			}
			encryptionType = s3.ServerSideEncryptionAwsKms
		} else {
			encryption = &s3.ServerSideEncryptionByDefault{
				SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256),
			}
			encryptionType = s3.ServerSideEncryptionAes256
		}

		_, err = svc.PutBucketEncryption(&s3.PutBucketEncryptionInput{
			Bucket: aws.String(d.Config.Bucket),
			ServerSideEncryptionConfiguration: &s3.ServerSideEncryptionConfiguration{
				Rules: []*s3.ServerSideEncryptionRule{
					{
						ApplyServerSideEncryptionByDefault: encryption,
					},
				},
			},
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				util.UpdateCondition(cr, imageregistryv1.StorageEncrypted, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			} else {
				util.UpdateCondition(cr, imageregistryv1.StorageEncrypted, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, imageregistryv1.StorageEncrypted, operatorapi.ConditionTrue, "Encryption Successful", fmt.Sprintf("Default %s encryption was successfully enabled on the S3 bucket", encryptionType))
			d.Config.Encrypt = true
			cr.Status.Storage.S3 = d.Config.DeepCopy()
			cr.Spec.Storage.S3 = d.Config.DeepCopy()
		}
	} else {
		if !reflect.DeepEqual(cr.Status.Storage.S3, d.Config) {
			cr.Status.Storage.S3 = d.Config.DeepCopy()
		}
	}

	// Enable default incomplete multipart upload cleanup after one (1) day
	if cr.Status.StorageManaged {
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
				util.UpdateCondition(cr, imageregistryv1.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			} else {
				util.UpdateCondition(cr, imageregistryv1.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, imageregistryv1.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionTrue, "Enable Cleanup Successful", "Default cleanup of incomplete multipart uploads after one (1) day was successfully enabled")
		}
	}

	return nil
}

// RemoveStorage deletes the storage medium that we created
func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (bool, error) {
	if !cr.Status.StorageManaged || len(d.Config.Bucket) == 0 {
		return false, nil
	}

	svc, err := d.getS3Service()
	if err != nil {
		return false, err
	}
	_, err = svc.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchBucket {
				util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, "S3 Bucket Deleted", "The S3 bucket did not exist.")
				return false, nil
			}
			util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionUnknown, aerr.Code(), aerr.Error())
			return false, err
		}
		return true, err
	}

	// Wait until the bucket does not exist
	if err := svc.WaitUntilBucketNotExists(&s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}); err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionTrue, aerr.Code(), aerr.Error())
		}

		return false, err
	}

	if len(cr.Spec.Storage.S3.Bucket) != 0 {
		cr.Spec.Storage.S3.Bucket = ""
	}

	d.Config.Bucket = ""

	if !reflect.DeepEqual(cr.Status.Storage.S3, d.Config) {
		cr.Status.Storage.S3 = d.Config.DeepCopy()
	}

	util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionFalse, "S3 Bucket Deleted", "The S3 bucket has been removed.")

	return false, nil
}
