package oss

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials/provider"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

const (
	imageRegistrySecretMountpoint = "/var/run/secrets/cloud"
	imageRegistrySecretDataKey    = "credentials"
)

var mutex sync.Mutex

type driver struct {
	Context     context.Context
	Config      *imageregistryv1.ImageRegistryConfigStorageAlibabaOSS
	Listers     *regopclient.Listers
	credentials *credentials.AccessKeyCredential

	// roundTripper is used only during tests.
	roundTripper http.RoundTripper
}

// NewDriver creates a new OSS storage driver
// Used during bootstrapping
func NewDriver(ctx context.Context, c *imageregistryv1.ImageRegistryConfigStorageAlibabaOSS, listers *regopclient.Listers) *driver {
	return &driver{
		Context: ctx,
		Config:  c,
		Listers: listers,
	}
}

// UpdateEffectiveConfig updates the driver's local effective OSS configuration
// based on infrastructure settings and any custom overrides.
func (d *driver) UpdateEffectiveConfig() error {
	effectiveConfig := d.Config.DeepCopy()

	if effectiveConfig == nil {
		effectiveConfig = &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{}
	}

	// Load infrastructure values
	infra, err := util.GetInfrastructure(d.Listers)
	if err != nil {
		return err
	}

	var clusterRegion string
	if infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.Type == configv1.AlibabaCloudPlatformType {
		clusterRegion = infra.Status.PlatformStatus.AlibabaCloud.Region
	}

	// Use cluster defaults when custom config doesn't define values
	if d.Config == nil || len(effectiveConfig.Region) == 0 {
		effectiveConfig.Region = clusterRegion
	}

	err = d.getCredentialsConfigData()
	if err != nil {
		return err
	}

	d.Config = effectiveConfig.DeepCopy()

	return nil
}

func fetchCredentialsIniFromSecret(secret *corev1.Secret) (auth.Credential, error) {
	mutex.Lock()
	defer mutex.Unlock()
	creds, ok := secret.Data[imageRegistrySecretDataKey]
	if !ok {
		return nil, fmt.Errorf("failed to fetch key 'credentials' in secret data")
	}

	f, err := ioutil.TempFile("", "alibaba-creds-*")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	defer os.Unsetenv(provider.ENVCredentialFile)
	_, err = f.Write(creds)
	if err != nil {
		return nil, err
	}
	os.Setenv(provider.ENVCredentialFile, f.Name())
	// use Alibaba provider initialization
	p := provider.NewProfileProvider("default")
	// return a valid auth credential
	return p.Resolve()
}

func (d *driver) getCredentialsConfigData() error {
	if d.credentials != nil {
		return nil
	}

	// Look for a user defined secret to get the Alibaba Cloud credentials from first
	sec, err := d.Listers.Secrets.Get(defaults.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
		// Fall back to those provided by the credential minter if nothing is provided by the user
		sec, err = d.Listers.Secrets.Get(defaults.CloudCredentialsName)
		if err != nil {
			return fmt.Errorf("unable to get cluster minted credentials %q: %v", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.CloudCredentialsName), err)
		}
	}

	credential, err := fetchCredentialsIniFromSecret(sec)
	if err != nil {
		return fmt.Errorf("failed to generate shared secrets data: %v", err)
	}
	// This must be an AccessKeyCrential because the oss.New requires accessKeyID and accessSecretKey
	d.credentials = credential.(*credentials.AccessKeyCredential)

	return nil
}

// isInternal return An internal endpoint or the public endpoint for OSS access.  default internal
func (d *driver) isInternal() bool {
	return d.Config.EndpointAccessibility != imageregistryv1.PublicEndpoint
}

// getOSSEndpoint returns an endpoint that allows us to interact
// with the Alibaba Cloud OSS service, details in https://www.alibabacloud.com/help/doc-detail/31837.htm
func (d *driver) getOSSEndpoint() string {
	if d.isInternal() {
		return fmt.Sprintf("oss-%s-internal.aliyuncs.com", d.Config.Region)
	}
	return fmt.Sprintf("oss-%s.aliyuncs.com", d.Config.Region)
}

func (d *driver) getOSSService() (*oss.Client, error) {
	err := d.getCredentialsConfigData()
	if err != nil {
		return nil, err
	}

	err = d.UpdateEffectiveConfig()
	if err != nil {
		return nil, err
	}

	endpoint := d.getOSSEndpoint()

	clientOptions := []oss.ClientOption{}
	if d.roundTripper != nil {
		clientOptions = append(clientOptions, oss.HTTPClient(&http.Client{Transport: d.roundTripper}))
	}
	client, err := oss.New(endpoint, d.credentials.AccessKeyId, d.credentials.AccessKeySecret, clientOptions...)
	if err != nil {
		return nil, err
	}

	return client, err
}

// ConfigEnv configures the environment variables
// Note: it is the callers responsiblity to make sure the returned file
// location is cleaned up after it is no longer needed.
func (d *driver) ConfigEnv() (envs envvar.List, err error) {
	err = d.UpdateEffectiveConfig()
	if err != nil {
		return
	}

	envs = append(envs,
		envvar.EnvVar{Name: "REGISTRY_STORAGE_OSS_ENDPOINT", Value: fmt.Sprintf("%s.%s", d.Config.Bucket, d.getOSSEndpoint())},
		envvar.EnvVar{Name: "REGISTRY_STORAGE", Value: "oss"},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_OSS_BUCKET", Value: d.Config.Bucket},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_OSS_REGION", Value: fmt.Sprintf("oss-%s", d.Config.Region)},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_OSS_INTERNAL", Value: d.isInternal()},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_OSS_ENCRYPT", Value: true},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_OSS_CREDENTIALSCONFIGPATH", Value: filepath.Join(imageRegistrySecretMountpoint, imageRegistrySecretDataKey)},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_OSS_ACCESSKEYID", Value: d.credentials.AccessKeyId},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_OSS_ACCESSKEYSECRET", Value: d.credentials.AccessKeySecret},
	)

	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

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

	return volumes, volumeMounts, nil
}

func (d *driver) VolumeSecrets() (map[string]string, error) {
	// Return the same credentials data that the image-registry-operator is using
	// so that it can be stored in the image-registry Pod's Secret.
	err := d.getCredentialsConfigData()
	if err != nil {
		return nil, err
	}

	confData, err := d.sharedCredentialsDataFromStaticCreds()
	if err != nil {
		return nil, err
	}

	return map[string]string{
		imageRegistrySecretDataKey: string(confData),
	}, nil
}

func (d *driver) sharedCredentialsDataFromStaticCreds() ([]byte, error) {
	if d.credentials == nil || d.credentials.AccessKeyId == "" || d.credentials.AccessKeySecret == "" {
		return []byte{}, fmt.Errorf("invalid credentials for Alibaba Cloud")
	}
	buf := &bytes.Buffer{}
	fmt.Fprint(buf, "[default]\n")
	fmt.Fprint(buf, "type = access_key\n")
	fmt.Fprintf(buf, "access_key_id = %s\n", d.credentials.AccessKeyId)
	fmt.Fprintf(buf, "access_key_secret = %s\n", d.credentials.AccessKeySecret)

	return buf.Bytes(), nil
}

// bucketExists checks whether or not the OSS bucket exists
func (d *driver) bucketExists(bucketName string) (bool, error) {
	if len(bucketName) == 0 {
		return false, nil
	}

	svc, err := d.getOSSService()
	if err != nil {
		return false, err
	}

	_, err = svc.GetBucketInfo(bucketName)

	if err != nil {
		return false, err
	}

	return true, nil
}

// StorageExists checks if an OSS bucket with the given name exists
// and we can access it
func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	if len(d.Config.Bucket) == 0 {
		return false, nil
	}

	bucketExists, err := d.bucketExists(d.Config.Bucket)
	if bucketExists {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "OSS Bucket Exists", "")
	}
	if err != nil {
		if oerr, ok := err.(oss.ServiceError); ok {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, oerr.Code, oerr.Error())
			return false, nil
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
		return false, err
	}

	return true, nil

}

// StorageChanged checks to see if the name of the storage medium
// has changed
func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.OSS, cr.Spec.Storage.OSS) {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "OSS Configuration Changed", "OSS storage is in an unknown state")
		return true
	}

	return false
}

// CreateStorage attempts to create an OSS bucket
// and apply any provided tags
func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	infra, err := util.GetInfrastructure(d.Listers)
	if err != nil {
		return err
	}

	if err := d.UpdateEffectiveConfig(); err != nil {
		return err
	}

	svc, err := d.getOSSService()
	if err != nil {
		return err
	}

	// If a bucket name is supplied, and it already exists and we can access it
	// just update the config
	var bucketExists bool
	if len(d.Config.Bucket) != 0 {
		bucketExists, err = d.bucketExists(d.Config.Bucket)
		if bucketExists {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "OSS Bucket Exists", "")
		}
		if err != nil {
			if oerr, ok := err.(oss.ServiceError); ok {
				switch oerr.Code {
				case "NoSuchBucket", "NotFound":
					// If the bucket doesn't exist that's ok, we'll try to create it
					klog.Infof("The bucket %s not found ", d.Config.Bucket)
				default:
					util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
					return err
				}
			} else {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "Unknown Error Occurred", err.Error())
				return err
			}
		}
	}

	if len(d.Config.Bucket) != 0 && bucketExists {
		if cr.Spec.Storage.ManagementState == "" {
			cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateUnmanaged
		}

		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			OSS: d.Config.DeepCopy(),
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "OSS Bucket Exists", "User supplied OSS bucket exists and is accessible")

	} else {
		generatedName := false
		// Retry up to 500 times if we get a naming conflict
		const numRetries = 500
		for i := 0; i < numRetries; i++ {
			// If the bucket name is blank, let's generate one
			if len(d.Config.Bucket) == 0 {
				if d.Config.Bucket, err = util.GenerateStorageName(d.Listers, d.Config.Region); err != nil {
					return err
				}
				generatedName = true
			}

			err := svc.CreateBucket(d.Config.Bucket)
			if err != nil {
				if oerr, ok := err.(oss.ServiceError); ok {
					switch oerr.Code {
					case "BucketAlreadyExists":
						if d.Config.Bucket != "" && !generatedName {
							util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Unable to Access Bucket", "The bucket exists, but we do not have permission to access it")
							break
						}
						d.Config.Bucket = ""
						continue
					default:
						util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, oerr.Code, oerr.Error())
						return err
					}
				}
			}
			if cr.Spec.Storage.ManagementState == "" {
				cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateManaged
			}
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				OSS: d.Config.DeepCopy(),
			}
			cr.Spec.Storage.OSS = d.Config.DeepCopy()
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Creation Successful", "OSS bucket was successfully created")

			break
		}

		if len(d.Config.Bucket) == 0 {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Unable to Generate Unique Bucket Name", "")
			return fmt.Errorf("unable to generate a unique OSS bucket name")
		}
	}

	// Block public access to the OSS bucket and its objects by default
	if cr.Spec.Storage.ManagementState == imageregistryv1.StorageManagementStateManaged {
		err := svc.SetBucketACL(d.Config.Bucket, oss.ACLPrivate)

		if err != nil {
			if oerr, ok := err.(oss.ServiceError); ok {
				util.UpdateCondition(cr, defaults.StoragePublicAccessBlocked, operatorapi.ConditionFalse, oerr.Code, oerr.Error())
			} else {
				util.UpdateCondition(cr, defaults.StoragePublicAccessBlocked, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, defaults.StoragePublicAccessBlocked, operatorapi.ConditionTrue, "Public Access Block Successful", "Public access to the OSS bucket and its contents have been successfully blocked.")
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				OSS: d.Config.DeepCopy(),
			}
			cr.Spec.Storage.OSS = d.Config.DeepCopy()
		}
	}

	// Tag the bucket with the openshiftClusterID
	// along with any user defined tags from the cluster configuration
	if cr.Spec.Storage.ManagementState == imageregistryv1.StorageManagementStateManaged {
		klog.Info("setting Alibaba Cloud OSS bucket tags")

		tagset := []oss.Tag{
			{
				Key:   "kubernetes.io/cluster/" + infra.Status.InfrastructureName,
				Value: "owned",
			},
			{
				Key:   "Name",
				Value: infra.Status.InfrastructureName + "-image-registry",
			},
			{
				Key:   "sigs.k8s.io/cloud-provider-alibaba/origin",
				Value: "ocp",
			},
			{
				Key:   "GISV",
				Value: "ocp",
			},
		}

		// at this stage we are not keeping user tags in sync. as per enhancement proposal
		// we only set user provided tags when we created the bucket.
		hasOSSStatus := infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.AlibabaCloud != nil
		if hasOSSStatus {
			klog.Infof("user provided %d tags", len(infra.Status.PlatformStatus.AlibabaCloud.ResourceTags))
			for _, tag := range infra.Status.PlatformStatus.AlibabaCloud.ResourceTags {
				klog.Infof("user provided bucket tag: %s: %s", tag.Key, tag.Value)
				tagset = append(tagset, oss.Tag{
					Key:   tag.Key,
					Value: tag.Value,
				})
			}
		}
		klog.V(5).Infof("tagging bucket with tags: %+v", tagset)

		tagging := oss.Tagging{
			Tags: tagset,
		}
		err := svc.SetBucketTagging(d.Config.Bucket, tagging)
		if err != nil {
			if oerr, ok := err.(oss.ServiceError); ok {
				util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionFalse, oerr.Code, oerr.Error())
			} else {
				util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionTrue, "Tagging Successful", "Tags were successfully applied to the OSS bucket")
		}
	} else {
		klog.Info("ignoring bucket tags, storage is not managed")
	}

	// Enable default encryption on the bucket
	if cr.Spec.Storage.ManagementState == imageregistryv1.StorageManagementStateManaged {
		encryptionRule := oss.ServerEncryptionRule{}

		// kms encryption
		if d.Config.Encryption != nil && d.Config.Encryption.Method == imageregistryv1.KMS && d.Config.Encryption.KMS != nil && len(d.Config.Encryption.KMS.KeyID) > 0 {
			encryptionRule.SSEDefault.SSEAlgorithm = string(oss.KMSAlgorithm)
			encryptionRule.SSEDefault.KMSMasterKeyID = d.Config.Encryption.KMS.KeyID
		} else {
			encryptionRule.SSEDefault.SSEAlgorithm = string(oss.AESAlgorithm)
		}

		err = svc.SetBucketEncryption(d.Config.Bucket, encryptionRule)
		if err != nil {
			if oerr, ok := err.(oss.ServiceError); ok {
				util.UpdateCondition(cr, defaults.StorageEncrypted, operatorapi.ConditionFalse, oerr.Code, oerr.Error())
			} else {
				util.UpdateCondition(cr, defaults.StorageEncrypted, operatorapi.ConditionFalse, "Unknown Error Occurred", err.Error())
			}
		} else {
			util.UpdateCondition(cr, defaults.StorageEncrypted, operatorapi.ConditionTrue, "Encryption Successful", fmt.Sprintf("Default %s encryption was successfully enabled on the OSS bucket", encryptionRule.SSEDefault.SSEAlgorithm))
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				OSS: d.Config.DeepCopy(),
			}
			cr.Spec.Storage.OSS = d.Config.DeepCopy()
		}
	} else {
		if !reflect.DeepEqual(cr.Status.Storage.OSS, d.Config) {
			cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
				OSS: d.Config.DeepCopy(),
			}
		}
	}

	// Enable default incomplete multipart upload cleanup after one (1) day

	if cr.Spec.Storage.ManagementState == imageregistryv1.StorageManagementStateManaged {
		rules := []oss.LifecycleRule{
			{
				ID:     "cleanup-incomplete-multipart-registry-uploads",
				Prefix: "",
				Status: "Enabled",
				AbortMultipartUpload: &oss.LifecycleAbortMultipartUpload{
					Days: 1,
				},
			},
		}
		err = svc.SetBucketLifecycle(d.Config.Bucket, rules)
		if err != nil {
			if oerr, ok := err.(oss.ServiceError); ok {
				util.UpdateCondition(cr, defaults.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionFalse, oerr.Code, oerr.Error())
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
// The OSS bucket must be empty before it can be removed
func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (bool, error) {
	if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged ||
		len(d.Config.Bucket) == 0 {
		return false, nil
	}

	svc, err := d.getOSSService()
	if err != nil {
		return false, err
	}

	// Get bucket
	bucket, err := svc.Bucket(d.Config.Bucket)
	if err != nil {
		return false, err
	}

	// Delete part
	keyMarker := oss.KeyMarker("")
	uploadIDMarker := oss.UploadIDMarker("")
	for {
		lmur, err := bucket.ListMultipartUploads(keyMarker, uploadIDMarker)
		if err != nil {
			return false, err
		}
		for _, upload := range lmur.Uploads {
			var imur = oss.InitiateMultipartUploadResult{Bucket: bucket.BucketName,
				Key: upload.Key, UploadID: upload.UploadID}
			err = bucket.AbortMultipartUpload(imur)
			if err != nil {
				return false, err
			}
		}
		keyMarker = oss.KeyMarker(lmur.NextKeyMarker)
		uploadIDMarker = oss.UploadIDMarker(lmur.NextUploadIDMarker)
		if !lmur.IsTruncated {
			break
		}
	}

	// Delete objects
	marker := oss.Marker("")
	for {
		lor, err := bucket.ListObjects(marker)
		if err != nil {
			return false, err
		}
		for _, object := range lor.Objects {
			err = bucket.DeleteObject(object.Key)
			if err != nil {
				return false, err
			}
		}
		marker = oss.Marker(lor.NextMarker)
		if !lor.IsTruncated {
			break
		}
	}

	// Delete bucket
	err = svc.DeleteBucket(d.Config.Bucket)

	if err != nil {
		if oerr, ok := err.(oss.ServiceError); ok {
			if oerr.Code == "NoSuchBucket" {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "OSS Bucket Deleted", "The OSS bucket did not exist.")
				return true, nil
			}
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, oerr.Code, oerr.Error())
			return false, err
		}
		return true, err
	}

	if len(cr.Spec.Storage.OSS.Bucket) != 0 {
		cr.Spec.Storage.OSS.Bucket = ""
	}

	d.Config.Bucket = ""

	if !reflect.DeepEqual(cr.Status.Storage.OSS, d.Config) {
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			OSS: d.Config.DeepCopy(),
		}
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "OSS Bucket Deleted", "The OSS bucket has been removed.")

	return false, nil
}

// ID return the underlying storage identificator, on this case the bucket name.
func (d *driver) ID() string {
	return d.Config.Bucket
}
