package s3

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"k8s.io/apimachinery/pkg/util/uuid"

	corev1 "k8s.io/api/core/v1"

	installer "github.com/openshift/installer/pkg/types"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

type driver struct {
	Name      string
	Namespace string
	Config    *opapi.ImageRegistryConfigStorageS3
}

func NewDriver(crname string, crnamespace string, c *opapi.ImageRegistryConfigStorageS3) *driver {
	return &driver{
		Name:      crname,
		Namespace: crnamespace,
		Config:    c,
	}
}

func (d *driver) GetName() string {
	return "s3"
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: d.GetName()},
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

// checkBucketExists checks if an S3 bucket with the given name exists
func (d *driver) checkBucketExists(svc *s3.S3) error {
	_, err := svc.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	})
	return err
}

// createBucket attempts to create an s3 bucket with the given name
func (d *driver) createAndTagBucket(svc *s3.S3, installConfig *installer.InstallConfig, customResourceStatus *opapi.ImageRegistryStatus) error {
	createBucketInput := &s3.CreateBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}

	// Create the S3 bucket
	if _, err := svc.CreateBucket(createBucketInput); err != nil {
		return err
	}

	// Wait until the bucket exists
	if err := svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}); err != nil {
		return err
	}

	// Tag the bucket with the openshiftClusterID
	// along with any user defined tags from the cluster configuration
	if installConfig.Platform.AWS != nil {
		var tagSet []*s3.Tag
		tagSet = append(tagSet, &s3.Tag{Key: aws.String("openshiftClusterID"), Value: aws.String(installConfig.ClusterID)})
		for k, v := range installConfig.Platform.AWS.UserTags {
			tagSet = append(tagSet, &s3.Tag{Key: aws.String(k), Value: aws.String(v)})
		}

		tagBucketInput := &s3.PutBucketTaggingInput{
			Bucket: aws.String(d.Config.Bucket),
			Tagging: &s3.Tagging{
				TagSet: tagSet,
			},
		}
		if _, err := svc.PutBucketTagging(tagBucketInput); err != nil {
			return err
		}
	}

	// Enable default encryption on the bucket
	defaultEncryption := &s3.ServerSideEncryptionByDefault{SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256)}
	encryptionRule := &s3.ServerSideEncryptionRule{ApplyServerSideEncryptionByDefault: defaultEncryption}
	encryptionRules := []*s3.ServerSideEncryptionRule{encryptionRule}
	encryptionConfig := &s3.ServerSideEncryptionConfiguration{Rules: encryptionRules}
	bucketEncryptionInput := &s3.PutBucketEncryptionInput{Bucket: aws.String(d.Config.Bucket), ServerSideEncryptionConfiguration: encryptionConfig}

	_, err := svc.PutBucketEncryption(bucketEncryptionInput)
	if err != nil {
		return err
	}

	customResourceStatus.Storage.Managed = true

	return nil
}

func (d *driver) createOrUpdatePrivateConfiguration(accessKey string, secretKey string) error {
	data := make(map[string]string)

	data["REGISTRY_STORAGE_S3_ACCESSKEY"] = accessKey
	data["REGISTRY_STORAGE_S3_SECRETKEY"] = secretKey

	return util.CreateOrUpdateSecret("image-registry", "openshift-image-registry", data)
}

func (d *driver) CompleteConfiguration(customResourceStatus *opapi.ImageRegistryStatus) error {
	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		return err
	}

	installConfig, err := clusterconfig.GetInstallConfig()
	if err != nil {
		return err
	}
	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(cfg.Storage.S3.AccessKey, cfg.Storage.S3.SecretKey, ""),
		Region:      &cfg.Storage.S3.Region,
	})
	if err != nil {
		return err
	}
	svc := s3.New(sess)

	if len(d.Config.Bucket) == 0 {
		d.Config.Bucket = cfg.Storage.S3.Bucket
	}
	if len(d.Config.Region) == 0 {
		d.Config.Region = cfg.Storage.S3.Region
	}

	if len(d.Config.Bucket) == 0 {
		for {
			d.Config.Bucket = fmt.Sprintf("%s-%s-%s-%s", clusterconfig.StoragePrefix, d.Config.Region, strings.Replace(installConfig.ClusterID, "-", "", -1), strings.Replace(string(uuid.NewUUID()), "-", "", -1))[0:62]
			if err := d.createAndTagBucket(svc, installConfig, customResourceStatus); err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case s3.ErrCodeBucketAlreadyExists:
						continue
					default:
						return err
					}
				}
			} else {
				break
			}
		}
	} else {
		if err := d.checkBucketExists(svc); err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case s3.ErrCodeNoSuchBucket:
					if err = d.createAndTagBucket(svc, installConfig, customResourceStatus); err != nil {
						return err
					}
				default:
					return err
				}
			}
		}
	}
	if err := d.createOrUpdatePrivateConfiguration(cfg.Storage.S3.AccessKey, cfg.Storage.S3.SecretKey); err != nil {
		return err
	}

	customResourceStatus.Storage.State.S3 = d.Config

	return nil
}
