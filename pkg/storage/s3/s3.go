package s3

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"golang.org/x/net/http/httpproxy"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	"github.com/openshift/cluster-image-registry-operator/pkg/version"
)

const (
	imageRegistrySecretMountpoint = "/var/run/secrets/cloud"
	imageRegistrySecretDataKey    = "credentials"
)

type driver struct {
	Context context.Context
	Config  *imageregistryv1.ImageRegistryConfigStorageS3
	Listers *regopclient.Listers

	// roundTripper is used only during tests.
	roundTripper http.RoundTripper
}

// NewDriver creates a new s3 storage driver
// Used during bootstrapping
func NewDriver(ctx context.Context, c *imageregistryv1.ImageRegistryConfigStorageS3, listers *regopclient.Listers) *driver {
	return &driver{
		Context: ctx,
		Config:  c,
		Listers: listers,
	}
}

// UpdateEffectiveConfig updates the driver's local effective ImageRegistryConfig and returns the effective image
// registry configuration based on infrastructure settings and any custom overrides.
func (d *driver) UpdateEffectiveConfig() (*imageregistryv1.ImageRegistryConfigStorageS3, error) {

	effectiveConfig := d.Config.DeepCopy()

	if effectiveConfig == nil {
		effectiveConfig = &imageregistryv1.ImageRegistryConfigStorageS3{}
	}

	// Load infrastructure values
	infra, err := util.GetInfrastructure(d.Listers)
	if err != nil {
		return nil, err
	}

	var clusterRegion, clusterRegionEndpoint string
	if infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.Type == configapiv1.AWSPlatformType {
		clusterRegion = infra.Status.PlatformStatus.AWS.Region

		for _, ep := range infra.Status.PlatformStatus.AWS.ServiceEndpoints {
			if ep.Name == "s3" {
				clusterRegionEndpoint = ep.URL
				break
			}
		}
	}

	// Use cluster defaults when custom config doesn't define values
	if d.Config == nil || (len(effectiveConfig.Region) == 0 && len(effectiveConfig.RegionEndpoint) == 0) {
		effectiveConfig.Region = clusterRegion
		effectiveConfig.RegionEndpoint = clusterRegionEndpoint
		if len(effectiveConfig.RegionEndpoint) != 0 {
			effectiveConfig.VirtualHostedStyle = true
		}
	}

	d.Config = effectiveConfig.DeepCopy()

	return effectiveConfig, nil
}

// GetCredentialsFile will create and return the location of an AWS config file that can
// be used to create AWS clients with. Caller is responsible for cleaning up the file.
// sharedCredentialsFile, err := d.GetCredentialsFile()
// if err != nil {
// 	// handle error
// }
// defer os.Remove(sharedCredentialsFile)
// options := session.Options{
// 	SharedConfigState: session.SharedConfigEnable,
// 	SharedConfigFiles: []string{sharedCredentialsFile},
// }
// sess := session.Must(session.NewSessionWithOptions(options))
func (d *driver) GetCredentialsFile() (string, error) {
	data, err := d.getCredentialsConfigData()
	if err != nil {
		return "", err
	}

	return saveSharedCredentialsFile(data)
}

func (d *driver) getCredentialsConfigData() ([]byte, error) {
	// Look for a user defined secret to get the AWS credentials from first
	sec, err := d.Listers.Secrets.Get(defaults.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
		// Fall back to those provided by the credential minter if nothing is provided by the user
		sec, err = d.Listers.Secrets.Get(defaults.CloudCredentialsName)
		if err != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials %q: %v", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.CloudCredentialsName), err)
		}

		data, err := sharedCredentialsDataFromSecret(sec)
		if err != nil {
			return nil, fmt.Errorf("failed to generate shared secrets data: %v", err)
		}
		return data, nil
	} else if err != nil {
		return nil, err
	} else {
		var accessKey, secretKey string
		if v, ok := sec.Data["REGISTRY_STORAGE_S3_ACCESSKEY"]; ok {
			accessKey = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_S3_ACCESSKEY\"", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser))
		}
		if v, ok := sec.Data["REGISTRY_STORAGE_S3_SECRETKEY"]; ok {
			secretKey = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_S3_SECRETKEY\"", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser))
		}

		return sharedCredentialsDataFromStaticCreds(accessKey, secretKey), nil
	}
}

// getCABundle gets the custom CA bundle for trusting communication with the AWS API
func (d *driver) getCABundle() (string, error) {
	cloudConfig, err := d.Listers.OpenShiftConfigManaged.Get(defaults.KubeCloudConfigName)
	switch {
	case errors.IsNotFound(err):
		// No cloud config, so no custom CA bundle.
		return "", nil
	case err != nil:
		return "", fmt.Errorf("unable to get the kube cloud config: %w", err)
	default:
		caBundle, ok := cloudConfig.Data[defaults.CloudCABundleKey]
		if !ok {
			return "", nil
		}
		return caBundle, nil
	}
}

// getS3Service returns a client that allows us to interact
// with the aws S3 service
func (d *driver) getS3Service() (*s3.S3, error) {
	credentialsFilename, err := d.GetCredentialsFile()
	if err != nil {
		return nil, err
	}
	defer os.Remove(credentialsFilename)

	_, err = d.UpdateEffectiveConfig()
	if err != nil {
		return nil, err
	}

	// A custom HTTPClient is used here since the default HTTPClients ProxyFromEnvironment
	// uses a cache which won't let us update the proxy env vars
	awsOptions := session.Options{
		Config: aws.Config{
			Region: &d.Config.Region,
			HTTPClient: &http.Client{
				Transport: &http.Transport{
					Proxy: func(req *http.Request) (*url.URL, error) {
						return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
					},
					DialContext: (&net.Dialer{
						Timeout:   30 * time.Second,
						KeepAlive: 30 * time.Second,
						DualStack: true,
					}).DialContext,
					ForceAttemptHTTP2:     true,
					MaxIdleConns:          100,
					IdleConnTimeout:       90 * time.Second,
					TLSHandshakeTimeout:   10 * time.Second,
					ExpectContinueTimeout: 1 * time.Second,
				},
			},
		},
		SharedConfigState: session.SharedConfigEnable,
		SharedConfigFiles: []string{credentialsFilename},
	}

	if d.roundTripper != nil {
		awsOptions.Config.HTTPClient.Transport = d.roundTripper
	}

	awsOptions.Config.WithUseDualStack(true)
	if d.Config.RegionEndpoint != "" {
		if !d.Config.VirtualHostedStyle {
			awsOptions.Config.WithS3ForcePathStyle(true)
		}
		awsOptions.Config.WithEndpoint(d.Config.RegionEndpoint)
	}

	switch caBundle, err := d.getCABundle(); {
	case err != nil:
		return nil, err
	case caBundle != "":
		awsOptions.CustomCABundle = strings.NewReader(caBundle)
	}

	sess, err := session.NewSessionWithOptions(awsOptions)
	if err != nil {
		return nil, err
	}
	sess.Handlers.Build.PushBackNamed(request.NamedHandler{
		Name: "openshift.io/cluster-image-registry-operator",
		Fn:   request.MakeAddToUserAgentHandler("openshift.io cluster-image-registry-operator", version.Version),
	})

	return s3.New(sess), nil
}

func isBucketNotFound(err interface{}) bool {
	switch s3Err := err.(type) {
	case awserr.Error:
		if s3Err.Code() == "NoSuchBucket" {
			return true
		}
		origErr := s3Err.OrigErr()
		if origErr != nil {
			return isBucketNotFound(origErr)
		}
	case s3manager.Error:
		if s3Err.OrigErr != nil {
			return isBucketNotFound(s3Err.OrigErr)
		}
	case s3manager.Errors:
		if len(s3Err) == 1 {
			return isBucketNotFound(s3Err[0])
		}
	}
	return false
}

// ConfigEnv configures the environment variables that will be
// used in the image registry deployment, and returns an AWS credentials file
// that can be used for setting up an AWS session/client.
// Note: it is the callers responsiblity to make sure the returned file
// location is cleaned up after it is no longer needed.
func (d *driver) ConfigEnv() (envs envvar.List, err error) {
	_, err = d.UpdateEffectiveConfig()
	if err != nil {
		return
	}

	if len(d.Config.RegionEndpoint) != 0 {
		envs = append(envs, envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_REGIONENDPOINT", Value: d.Config.RegionEndpoint})
	}

	if len(d.Config.KeyID) != 0 {
		envs = append(envs, envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_KEYID", Value: d.Config.KeyID})
	}

	envs = append(envs,
		envvar.EnvVar{Name: "REGISTRY_STORAGE", Value: "s3"},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_BUCKET", Value: d.Config.Bucket},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_REGION", Value: d.Config.Region},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_ENCRYPT", Value: d.Config.Encrypt},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_VIRTUALHOSTEDSTYLE", Value: d.Config.VirtualHostedStyle},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_USEDUALSTACK", Value: true},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_CREDENTIALSCONFIGPATH", Value: filepath.Join(imageRegistrySecretMountpoint, imageRegistrySecretDataKey)},
	)

	if d.Config.CloudFront != nil {
		envs = append(envs,
			envvar.EnvVar{Name: "REGISTRY_MIDDLEWARE_STORAGE_CLOUDFRONT_BASEURL", Value: d.Config.CloudFront.BaseURL},
			envvar.EnvVar{Name: "REGISTRY_MIDDLEWARE_STORAGE_CLOUDFRONT_KEYPAIRID", Value: d.Config.CloudFront.KeypairID},
			envvar.EnvVar{Name: "REGISTRY_MIDDLEWARE_STORAGE_CLOUDFRONT_DURATION", Value: d.Config.CloudFront.Duration.String()},
			envvar.EnvVar{Name: "REGISTRY_MIDDLEWARE_STORAGE_CLOUDFRONT_PRIVATEKEY", Value: "/etc/docker/cloudfront/private.pem"},
		)
	}

	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	volumes := []corev1.Volume{}
	volumeMounts := []corev1.VolumeMount{}

	optional := false

	// Mount the registry config secret containing the credentials file data
	credsVolume := corev1.Volume{
		Name: defaults.ImageRegistryPrivateConfiguration,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: defaults.ImageRegistryPrivateConfiguration,
				Optional:   &optional,
			},
		},
	}

	credsVolumeMount := corev1.VolumeMount{
		Name:      credsVolume.Name,
		MountPath: imageRegistrySecretMountpoint,
		ReadOnly:  true,
	}

	volumes = append(volumes, credsVolume)
	volumeMounts = append(volumeMounts, credsVolumeMount)

	if d.Config.CloudFront != nil {
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

		volumes = append(volumes, vol)

		mount := corev1.VolumeMount{
			Name:      vol.Name,
			MountPath: "/etc/docker/cloudfront",
			ReadOnly:  true,
		}

		volumeMounts = append(volumeMounts, mount)
	}

	return volumes, volumeMounts, nil
}

func (d *driver) VolumeSecrets() (map[string]string, error) {
	// Return the same credentials data that the image-registry-operator is using
	// so that it can be stored in the image-registry Pod's Secret.
	confData, err := d.getCredentialsConfigData()
	if err != nil {
		return nil, err
	}

	return map[string]string{
		imageRegistrySecretDataKey: string(confData),
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

	_, err = svc.HeadBucketWithContext(d.Context, &s3.HeadBucketInput{
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
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
				return false, nil
			}
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
		return false, err
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists", "")
	return true, nil

}

// StorageChanged checks to see if the name of the storage medium
// has changed
func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.S3, cr.Spec.Storage.S3) {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "S3 Configuration Changed", "S3 storage is in an unknown state")
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

	infra, err := util.GetInfrastructure(d.Listers)
	if err != nil {
		return err
	}

	if _, err := d.UpdateEffectiveConfig(); err != nil {
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
					util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
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
		if cr.Spec.Storage.ManagementState == "" {
			cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateUnmanaged
		}

		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			S3: d.Config.DeepCopy(),
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists", "User supplied S3 bucket exists and is accessible")

	} else {
		generatedName := false
		// Retry up to 5000 times if we get a naming conflict
		const numRetries = 5000
		for i := 0; i < numRetries; i++ {
			// If the bucket name is blank, let's generate one
			if len(d.Config.Bucket) == 0 {
				if d.Config.Bucket, err = util.GenerateStorageName(d.Listers, d.Config.Region); err != nil {
					return err
				}
				generatedName = true
			}

			_, err := svc.CreateBucketWithContext(d.Context, &s3.CreateBucketInput{
				Bucket: aws.String(d.Config.Bucket),
			})
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case s3.ErrCodeBucketAlreadyExists:
						if d.Config.Bucket != "" && !generatedName {
							util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Unable to Access Bucket", "The bucket exists, but we do not have permission to access it")
							break
						}
						d.Config.Bucket = ""
						continue
					default:
						util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
						return err
					}
				}
			}
			if cr.Spec.Storage.ManagementState == "" {
				cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateManaged
			}
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				S3: d.Config.DeepCopy(),
			}
			cr.Spec.Storage.S3 = d.Config.DeepCopy()

			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Creation Successful", "S3 bucket was successfully created")

			break
		}

		if len(d.Config.Bucket) == 0 {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Unable to Generate Unique Bucket Name", "")
			return fmt.Errorf("unable to generate a unique s3 bucket name")
		}
	}

	// Wait until the bucket exists
	if err := svc.WaitUntilBucketExistsWithContext(d.Context, &s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}); err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
		}

		return err
	}

	// Block public access to the s3 bucket and its objects by default
	if cr.Spec.Storage.ManagementState == imageregistryv1.StorageManagementStateManaged {
		_, err := svc.PutPublicAccessBlockWithContext(d.Context, &s3.PutPublicAccessBlockInput{
			Bucket: aws.String(d.Config.Bucket),
			PublicAccessBlockConfiguration: &s3.PublicAccessBlockConfiguration{
				BlockPublicAcls:       aws.Bool(true),
				BlockPublicPolicy:     aws.Bool(true),
				IgnorePublicAcls:      aws.Bool(true),
				RestrictPublicBuckets: aws.Bool(true),
			},
		})

		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				util.UpdateCondition(cr, defaults.StoragePublicAccessBlocked, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			} else {
				util.UpdateCondition(cr, defaults.StoragePublicAccessBlocked, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, defaults.StoragePublicAccessBlocked, operatorapi.ConditionTrue, "Public Access Block Successful", "Public access to the S3 bucket and its contents have been successfully blocked.")
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				S3: d.Config.DeepCopy(),
			}
			cr.Spec.Storage.S3 = d.Config.DeepCopy()
		}
	}

	// Tag the bucket with the openshiftClusterID
	// along with any user defined tags from the cluster configuration
	if cr.Spec.Storage.ManagementState == imageregistryv1.StorageManagementStateManaged {
		_, err := svc.PutBucketTaggingWithContext(d.Context, &s3.PutBucketTaggingInput{
			Bucket: aws.String(d.Config.Bucket),
			Tagging: &s3.Tagging{

				TagSet: []*s3.Tag{
					{
						Key:   aws.String("kubernetes.io/cluster/" + infra.Status.InfrastructureName),
						Value: aws.String("owned"),
					},
					{
						Key:   aws.String("Name"),
						Value: aws.String(infra.Status.InfrastructureName + "-image-registry"),
					},
				},
			},
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			} else {
				util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionTrue, "Tagging Successful", "Tags were successfully applied to the S3 bucket")
		}
	}

	// Enable default encryption on the bucket
	if cr.Spec.Storage.ManagementState == imageregistryv1.StorageManagementStateManaged {
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

		_, err = svc.PutBucketEncryptionWithContext(d.Context, &s3.PutBucketEncryptionInput{
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
				util.UpdateCondition(cr, defaults.StorageEncrypted, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			} else {
				util.UpdateCondition(cr, defaults.StorageEncrypted, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, defaults.StorageEncrypted, operatorapi.ConditionTrue, "Encryption Successful", fmt.Sprintf("Default %s encryption was successfully enabled on the S3 bucket", encryptionType))
			d.Config.Encrypt = true
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				S3: d.Config.DeepCopy(),
			}
			cr.Spec.Storage.S3 = d.Config.DeepCopy()
		}
	} else {
		if !reflect.DeepEqual(cr.Status.Storage.S3, d.Config) {
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				S3: d.Config.DeepCopy(),
			}
		}
	}

	// Enable default incomplete multipart upload cleanup after one (1) day
	if cr.Spec.Storage.ManagementState == imageregistryv1.StorageManagementStateManaged {
		_, err = svc.PutBucketLifecycleConfigurationWithContext(d.Context, &s3.PutBucketLifecycleConfigurationInput{
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
				util.UpdateCondition(cr, defaults.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			} else {
				util.UpdateCondition(cr, defaults.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, defaults.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionTrue, "Enable Cleanup Successful", "Default cleanup of incomplete multipart uploads after one (1) day was successfully enabled")
		}
	}

	return nil
}

// RemoveStorage deletes the storage medium that we created
// The s3 bucket must be empty before it can be removed
func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (bool, error) {
	if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged ||
		len(d.Config.Bucket) == 0 {
		return false, nil
	}

	svc, err := d.getS3Service()
	if err != nil {
		return false, err
	}

	iter := s3manager.NewDeleteListIterator(svc, &s3.ListObjectsInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	err = s3manager.NewBatchDeleteWithClient(svc).Delete(d.Context, iter)
	if err != nil && !isBucketNotFound(err) {
		return false, err
	}

	_, err = svc.DeleteBucketWithContext(d.Context, &s3.DeleteBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchBucket {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "S3 Bucket Deleted", "The S3 bucket did not exist.")
				return false, nil
			}
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, aerr.Code(), aerr.Error())
			return false, err
		}
		return true, err
	}

	// Wait until the bucket does not exist
	if err := svc.WaitUntilBucketNotExistsWithContext(d.Context, &s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}); err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, aerr.Code(), aerr.Error())
		}

		return false, err
	}

	if len(cr.Spec.Storage.S3.Bucket) != 0 {
		cr.Spec.Storage.S3.Bucket = ""
	}

	d.Config.Bucket = ""

	if !reflect.DeepEqual(cr.Status.Storage.S3, d.Config) {
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			S3: d.Config.DeepCopy(),
		}
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "S3 Bucket Deleted", "The S3 bucket has been removed.")

	return false, nil
}

// ID return the underlying storage identificator, on this case the bucket name.
func (d *driver) ID() string {
	return d.Config.Bucket
}

// saveSharedCredentialsFile will create a file with the provided data expected to be
// an AWS ini-style credentials configuration file.
// Caller is responsible for cleaning up the created file.
func saveSharedCredentialsFile(data []byte) (string, error) {
	f, err := ioutil.TempFile("", "aws-shared-credentials")
	if err != nil {
		return "", fmt.Errorf("failed to create file for shared credentials: %v", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		defer os.Remove(f.Name())
		return "", fmt.Errorf("failed to write credentials to %s: %v", f.Name(), err)
	}
	return f.Name(), nil
}

func sharedCredentialsDataFromSecret(secret *corev1.Secret) ([]byte, error) {
	switch {
	case len(secret.Data["credentials"]) > 0:
		return secret.Data["credentials"], nil
	case len(secret.Data["aws_access_key_id"]) > 0 && len(secret.Data["aws_secret_access_key"]) > 0:
		accessKey := string(secret.Data["aws_access_key_id"])
		secretKey := string(secret.Data["aws_secret_access_key"])
		return sharedCredentialsDataFromStaticCreds(accessKey, secretKey), nil
	default:
		return nil, fmt.Errorf("invalid secret for aws credentials")
	}
}

func sharedCredentialsDataFromStaticCreds(accessKey, accessSecret string) []byte {
	buf := &bytes.Buffer{}
	fmt.Fprint(buf, "[default]\n")
	fmt.Fprintf(buf, "aws_access_key_id = %s\n", accessKey)
	fmt.Fprintf(buf, "aws_secret_access_key = %s\n", accessSecret)

	return buf.Bytes()
}
