package s3

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"k8s.io/apimachinery/pkg/util/uuid"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	oputil "github.com/openshift/cluster-image-registry-operator/pkg/util"
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
func (d *driver) createBucket(svc *s3.S3) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}
	if _, err := svc.CreateBucket(input); err != nil {
		return err
	}

	err := svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	return err
}

func (d *driver) createOrUpdatePrivateConfiguration(accessKey string, secretKey string) error {
	data := make(map[string]string)

	data["REGISTRY_STORAGE_S3_ACCESSKEY"] = accessKey
	data["REGISTRY_STORAGE_S3_SECRETKEY"] = secretKey

	return util.CreateOrUpdateSecret("image-registry", "openshift-image-registry", data)
}

func (d *driver) CompleteConfiguration() error {
	cfg, err := oputil.GetAWSConfig()
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
			d.Config.Bucket = fmt.Sprintf("%s-%s", util.STORAGE_PREFIX, string(uuid.NewUUID()))
			if err := d.createBucket(svc); err != nil {
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
					if err = d.createBucket(svc); err != nil {
						return err
					}
				default:
					return err
				}
			}
		}
	}
	return d.createOrUpdatePrivateConfiguration(cfg.Storage.S3.AccessKey, cfg.Storage.S3.SecretKey)
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

	if v, ok := util.GetStateValue(&cr.Status, "s3-bucket"); ok {
		if v != d.Config.Bucket {
			return fmt.Errorf("S3 bucket change is not supported: expected bucket %s, but got %s", v, d.Config.Bucket)
		}
	} else {
		util.SetStateValue(&cr.Status, "s3-bucket", d.Config.Bucket)
		*modified = true
	}

	if v, ok := util.GetStateValue(&cr.Status, "s3-region"); ok {
		if v != d.Config.Region {
			return fmt.Errorf("S3 region change is not supported: expected region %s, but got %s", v, d.Config.Region)
		}
	} else {
		util.SetStateValue(&cr.Status, "s3-region", d.Config.Region)
		*modified = true
	}

	return nil
}
