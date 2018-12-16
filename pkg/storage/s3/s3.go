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

	operatorapi "github.com/openshift/api/operator/v1alpha1"
	installer "github.com/openshift/installer/pkg/types"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

var (
	s3Service     *s3.S3
	installConfig *installer.InstallConfig
)

type driver struct {
	Name      string
	Namespace string
	Config    *opapi.ImageRegistryConfigStorageS3
}

func (d *driver) getInstallConfig() (*installer.InstallConfig, error) {
	if installConfig != nil {
		return installConfig, nil
	}

	installConfig, err := clusterconfig.GetInstallConfig()
	if err != nil {
		return nil, err
	}

	return installConfig, nil
}

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

func (d *driver) GetName() string {
	return string(clusterconfig.StorageTypeS3)
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

// StorageExists checks if an S3 bucket with the given name exists
// and we can access it
func (d *driver) StorageExists(cr *opapi.ImageRegistry) error {
	svc, err := d.getSVC()
	if err != nil {
		return err
	}
	_, err = svc.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
		}
	}

	util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists", "")

	return err

}

// CreateStorage attempts to create an s3 bucket with the given name
// and apply any provided tags
func (d *driver) CreateStorage(cr *opapi.ImageRegistry) error {
	svc, err := d.getSVC()
	if err != nil {
		return err
	}

	ic, err := d.getInstallConfig()
	if err != nil {
		return err
	}

	createBucketInput := &s3.CreateBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}

	// Create the S3 bucket
	if _, err := svc.CreateBucket(createBucketInput); err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
		}

		return err
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
		tagSet = append(tagSet, &s3.Tag{Key: aws.String("openshiftClusterID"), Value: aws.String(ic.ClusterID)})
		for k, v := range ic.Platform.AWS.UserTags {
			tagSet = append(tagSet, &s3.Tag{Key: aws.String(k), Value: aws.String(v)})
		}

		tagBucketInput := &s3.PutBucketTaggingInput{
			Bucket: aws.String(d.Config.Bucket),
			Tagging: &s3.Tagging{
				TagSet: tagSet,
			},
		}
		if _, err := svc.PutBucketTagging(tagBucketInput); err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			}

			return err
		}
	}

	// Enable default encryption on the bucket
	defaultEncryption := &s3.ServerSideEncryptionByDefault{SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256)}
	encryptionRule := &s3.ServerSideEncryptionRule{ApplyServerSideEncryptionByDefault: defaultEncryption}
	encryptionRules := []*s3.ServerSideEncryptionRule{encryptionRule}
	encryptionConfig := &s3.ServerSideEncryptionConfiguration{Rules: encryptionRules}
	bucketEncryptionInput := &s3.PutBucketEncryptionInput{Bucket: aws.String(d.Config.Bucket), ServerSideEncryptionConfiguration: encryptionConfig}

	_, err = svc.PutBucketEncryption(bucketEncryptionInput)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
		}

		return err
	}

	cr.Status.Storage.Managed = true

	util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists", "")

	return nil
}

// RemoveStorage deletes the storage medium that we created
func (d *driver) RemoveStorage(cr *opapi.ImageRegistry) error {
	if !cr.Status.Storage.Managed {
		return fmt.Errorf("the S3 bucket is not managed by the image registry operator, so we can't delete it.")
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
			util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionTrue, aerr.Code(), aerr.Error())
		}
	}

	util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionFalse, "S3 Bucket Deleted", "The S3 bucket has been removed.")

	return err

}

func (d *driver) createOrUpdatePrivateConfiguration(accessKey string, secretKey string) error {
	operatorNamespace, err := regopclient.GetWatchNamespace()
	if err != nil {
		return err
	}

	data := map[string]string{
		"REGISTRY_STORAGE_S3_ACCESSKEY": accessKey,
		"REGISTRY_STORAGE_S3_SECRETKEY": secretKey,
	}

	_, err = util.CreateOrUpdateSecret(opapi.ImageRegistryPrivateConfiguration, operatorNamespace, data)

	return err
}

func (d *driver) CompleteConfiguration(cr *opapi.ImageRegistry) error {
	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		return err
	}

	ic, err := d.getInstallConfig()
	if err != nil {
		return err
	}

	if len(d.Config.Bucket) == 0 {
		d.Config.Bucket = cfg.Storage.S3.Bucket
	}
	if len(d.Config.Region) == 0 {
		d.Config.Region = cfg.Storage.S3.Region
	}

	if len(d.Config.Bucket) == 0 {
		for {
			d.Config.Bucket = fmt.Sprintf("%s-%s-%s-%s", clusterconfig.StoragePrefix, d.Config.Region, strings.Replace(ic.ClusterID, "-", "", -1), strings.Replace(string(uuid.NewUUID()), "-", "", -1))[0:62]
			if err := d.CreateStorage(cr); err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case s3.ErrCodeBucketAlreadyExists:
						continue
					default:
						d.Config.Bucket = ""
						return err
					}
				}
			} else {
				break
			}
		}
	} else {
		if err := d.StorageExists(cr); err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case s3.ErrCodeNoSuchBucket:
					if err = d.CreateStorage(cr); err != nil {
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

	if err := d.StorageExists(cr); err != nil {
		return err
	}
	cr.Status.Storage.State.S3 = d.Config

	return nil
}
