package ibmcos

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"time"

	"golang.org/x/net/http/httpproxy"
	"k8s.io/apimachinery/pkg/api/errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam"
	"github.com/IBM/ibm-cos-sdk-go/aws/request"
	"github.com/IBM/ibm-cos-sdk-go/aws/session"
	"github.com/IBM/ibm-cos-sdk-go/service/s3"
	"github.com/IBM/ibm-cos-sdk-go/service/s3/s3manager"
	"github.com/IBM/platform-services-go-sdk/resourcecontrollerv2"
	"github.com/IBM/platform-services-go-sdk/resourcemanagerv2"
	"github.com/golang-jwt/jwt"
	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	"github.com/openshift/cluster-image-registry-operator/pkg/version"
	powerUtils "github.com/ppc64le-cloud/powervs-utils"
)

const (
	// IBMTokenPath is the URI path for the token endpoint
	IAMTokenPath = "/identity/token"
	// IAMEndpoint is the default IAM token endpoint
	IAMEndpoint = "https://iam.cloud.ibm.com/identity/token"

	cosEndpointTemplate           = "s3.%s.cloud-object-storage.appdomain.cloud"
	imageRegistrySecretDataKey    = "credentials"
	imageRegistrySecretMountpoint = "/var/run/secrets/cloud"
)

type driver struct {
	AccountID string
	Context   context.Context
	Config    *imageregistryv1.ImageRegistryConfigStorageIBMCOS
	Listers   *regopclient.StorageListers

	// roundTripper is used only during tests.
	roundTripper http.RoundTripper

	// IBM Services used only during tests.
	resourceController *resourcecontrollerv2.ResourceControllerV2
	resourceManager    *resourcemanagerv2.ResourceManagerV2

	// Endpoints to use for IBM Cloud Services
	iamServiceEndpoint string
	cosServiceEndpoint string
	rcServiceEndpoint  string
	rmServiceEndpoint  string
}

// NewDriver creates a new IBM COS storage driver.
// Used during bootstrapping.
func NewDriver(ctx context.Context, c *imageregistryv1.ImageRegistryConfigStorageIBMCOS, listers *regopclient.StorageListers) *driver {
	return &driver{
		Context: ctx,
		Config:  c,
		Listers: listers,
	}
}

// CABundle returns a additional CA bundle for IBM COS.
func (d *driver) CABundle() (string, bool, error) {
	return "", true, nil
}

// ConfigEnv configures the environment variables that will be
// used in the image registry deployment.
func (d *driver) ConfigEnv() (envs envvar.List, err error) {
	_, err = d.UpdateEffectiveConfig()
	if err != nil {
		return
	}
	// Build the regional COS endpoint, or use the override endpoint if one was provided
	regionEndpoint := fmt.Sprintf(cosEndpointTemplate, d.Config.Location)
	if d.cosServiceEndpoint != "" {
		// We expect the override already is region specific
		regionEndpoint = d.cosServiceEndpoint
	}

	envs = append(envs,
		envvar.EnvVar{Name: "REGISTRY_STORAGE", Value: "s3"},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_BUCKET", Value: d.Config.Bucket},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_REGION", Value: d.Config.Location},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_REGIONENDPOINT", Value: regionEndpoint},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_ENCRYPT", Value: false},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_FORCEPATHSTYLE", Value: true},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_USEDUALSTACK", Value: false},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_S3_CREDENTIALSCONFIGPATH", Value: filepath.Join(imageRegistrySecretMountpoint, imageRegistrySecretDataKey)},
	)
	return
}

// UpdateEffectiveConfig updates the driver's local effective ImageRegistryConfig and returns the effective image
// registry configuration based on infrastructure settings and any custom overrides.
func (d *driver) UpdateEffectiveConfig() (*imageregistryv1.ImageRegistryConfigStorageIBMCOS, error) {
	effectiveConfig := d.Config.DeepCopy()

	if effectiveConfig == nil {
		effectiveConfig = &imageregistryv1.ImageRegistryConfigStorageIBMCOS{}
	}

	// Load infrastructure values
	infra, err := util.GetInfrastructure(d.Listers.Infrastructures)
	if err != nil {
		return nil, err
	}

	var clusterLocation string
	if infra.Status.PlatformStatus != nil {
		if infra.Status.PlatformStatus.Type == configapiv1.IBMCloudPlatformType && infra.Status.PlatformStatus.IBMCloud != nil {
			clusterLocation = infra.Status.PlatformStatus.IBMCloud.Location
		}
		if infra.Status.PlatformStatus.Type == configapiv1.PowerVSPlatformType && infra.Status.PlatformStatus.PowerVS != nil {
			clusterLocation, err = powerUtils.COSRegionForPowerVSRegion(infra.Status.PlatformStatus.PowerVS.Region)
			if err != nil {
				return nil, err
			}
		}
	}
	d.setServiceEndpointOverrides(infra)

	// Use cluster defaults when custom config doesn't define values
	if d.Config == nil || (len(effectiveConfig.Location) == 0) {
		effectiveConfig.Location = clusterLocation
	}

	d.Config = effectiveConfig.DeepCopy()

	return effectiveConfig, nil
}

// CreateStorage attempts to create an IBM COS service instance,
// resource key, and bucket.
func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	// Get Infrastructure spec
	infra, err := util.GetInfrastructure(d.Listers.Infrastructures)
	if err != nil {
		return err
	}

	// Set configs from Infrastructure
	if infra.Status.PlatformStatus != nil {
		if infra.Status.PlatformStatus.Type == configapiv1.IBMCloudPlatformType && infra.Status.PlatformStatus.IBMCloud != nil {
			d.Config.Location = infra.Status.PlatformStatus.IBMCloud.Location
			d.Config.ResourceGroupName = infra.Status.PlatformStatus.IBMCloud.ResourceGroupName
		}
		if infra.Status.PlatformStatus.Type == configapiv1.PowerVSPlatformType && infra.Status.PlatformStatus.PowerVS != nil {
			d.Config.Location, err = powerUtils.COSRegionForPowerVSRegion(infra.Status.PlatformStatus.PowerVS.Region)
			if err != nil {
				return err
			}
			d.Config.ResourceGroupName = infra.Status.PlatformStatus.PowerVS.ResourceGroup
		}
	}

	// Initialize IBMCOS status
	if cr.Status.Storage.IBMCOS == nil {
		cr.Status.Storage.IBMCOS = &imageregistryv1.ImageRegistryConfigStorageIBMCOS{}
	}

	// Get resource controller service
	rc, err := d.getResourceControllerService()
	if err != nil {
		return err
	}

	// Get resource manager service
	rm, err := d.getResourceManagerService()
	if err != nil {
		return err
	}

	// Check if service instance exists
	if len(d.Config.ServiceInstanceCRN) != 0 {
		instance, resp, err := rc.GetResourceInstanceWithContext(
			d.Context,
			&resourcecontrollerv2.GetResourceInstanceOptions{
				ID: &d.Config.ServiceInstanceCRN,
			},
		)
		if err != nil {
			return fmt.Errorf("unable to get resource instance: %s with resp code: %d", err.Error(), resp.StatusCode)
		}

		switch *instance.State {
		case resourcecontrollerv2.ListResourceInstancesOptionsStateActiveConst:
			// Service instance exists and is active
			if *instance.ResourceGroupID != "" {
				// Get resource group name
				rg, resp, err := rm.GetResourceGroupWithContext(
					d.Context,
					&resourcemanagerv2.GetResourceGroupOptions{
						ID: instance.ResourceGroupID,
					},
				)
				if err != nil {
					return fmt.Errorf("unable to get resource group: %s with resp code: %d", err.Error(), resp.StatusCode)
				}
				// Set resource group name
				d.Config.ResourceGroupName = *rg.Name
			}
			cr.Status.Storage.IBMCOS.ServiceInstanceCRN = d.Config.ServiceInstanceCRN
			cr.Status.Storage.IBMCOS.ResourceGroupName = d.Config.ResourceGroupName
			cr.Spec.Storage.IBMCOS = d.Config.DeepCopy()
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "IBM COS Instance Active", "IBM COS service instance is active")
		case resourcecontrollerv2.ListResourceInstancesOptionsStateProvisioningConst:
			// Service instance exists and is provisioning
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "IBM COS Instance Provisioning", "IBM COS service instance is provisioning")
			return fmt.Errorf("waiting for IBM COS service instance to finish provisioning")
		default:
			// Service instance does not exist, will create one
			d.Config.ServiceInstanceCRN = ""
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "IBM COS Instance Gone", "IBM COS service instance is inactive or has been removed.")
		}
	}

	// Attempt to create a new service instance
	if len(d.Config.ServiceInstanceCRN) == 0 {
		// Get account ID
		if d.AccountID == "" {
			d.AccountID, err = d.getAccountID()
			if err != nil {
				return fmt.Errorf("unable to determine account ID: %s", err.Error())
			}
		}

		// Get resource group details
		resourceGroups, resp, err := rm.ListResourceGroupsWithContext(
			d.Context,
			&resourcemanagerv2.ListResourceGroupsOptions{
				AccountID: &d.AccountID,
				Name:      &d.Config.ResourceGroupName,
			},
		)
		if resourceGroups == nil || err != nil {
			if resp != nil {
				return fmt.Errorf("unable to get resource groups: %s with resp code: %d", err.Error(), resp.StatusCode)
			}
			return fmt.Errorf("unable to get resource groups: %w using ResourceManager endpoint: %s", err, rm.GetServiceURL())
		} else if len(resourceGroups.Resources) == 0 {
			return fmt.Errorf("unable to find any resource groups with resp code: %d", resp.StatusCode)
		}

		// Define instance options
		serviceInstanceName := fmt.Sprintf("%s-%s", infra.Status.InfrastructureName, defaults.ImageRegistryName)
		serviceTarget := "bluemix-global"
		resourceGroupID := *resourceGroups.Resources[0].ID
		resourcePlanID := "744bfc56-d12c-4866-88d5-dac9139e0e5d"

		// Check if service instance with name already exists
		instances, resp, err := rc.ListResourceInstancesWithContext(
			d.Context,
			&resourcecontrollerv2.ListResourceInstancesOptions{
				Name:            &serviceInstanceName,
				ResourceGroupID: &resourceGroupID,
				ResourcePlanID:  &resourcePlanID,
			},
		)
		if err != nil {
			return fmt.Errorf("unable to get resource instances: %s with resp code: %d", err.Error(), resp.StatusCode)
		}

		var instance *resourcecontrollerv2.ResourceInstance
		if len(instances.Resources) != 0 {
			// Service instance found
			instance = &instances.Resources[0]
		} else {
			// Create COS service instance
			instance, resp, err = rc.CreateResourceInstanceWithContext(
				d.Context,
				&resourcecontrollerv2.CreateResourceInstanceOptions{
					Name:           &serviceInstanceName,
					Target:         &serviceTarget,
					ResourceGroup:  &resourceGroupID,
					ResourcePlanID: &resourcePlanID,
					Tags:           []string{fmt.Sprintf("kubernetes.io_cluster_%s:owned", infra.Status.InfrastructureName)},
				},
			)
			if err != nil {
				return fmt.Errorf("unable to create resource instance: %s with resp code: %d", err.Error(), resp.StatusCode)
			}

			if cr.Spec.Storage.ManagementState == "" {
				cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateManaged
			}
		}

		d.Config.ServiceInstanceCRN = *instance.CRN
		cr.Status.Storage.IBMCOS.ServiceInstanceCRN = d.Config.ServiceInstanceCRN
		cr.Status.Storage.IBMCOS.ResourceGroupName = d.Config.ResourceGroupName
		cr.Status.Storage.ManagementState = cr.Spec.Storage.ManagementState
		cr.Spec.Storage.IBMCOS = d.Config.DeepCopy()
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "IBM COS Instance Creation Successful", "IBM COS service instance was successfully created")
	}

	if len(d.Config.ResourceKeyCRN) == 0 {
		// Create resource key
		keyName := fmt.Sprintf("%s-%s", infra.Status.InfrastructureName, defaults.ImageRegistryName)
		roleCRN := "crn:v1:bluemix:public:iam::::serviceRole:Writer"
		params := &resourcecontrollerv2.ResourceKeyPostParameters{}
		params.SetProperty("HMAC", true)

		key, resp, err := rc.CreateResourceKeyWithContext(
			d.Context,
			&resourcecontrollerv2.CreateResourceKeyOptions{
				Name:       &keyName,
				Source:     &d.Config.ServiceInstanceCRN,
				Role:       &roleCRN,
				Parameters: params,
			},
		)
		if err != nil {
			return fmt.Errorf("unable to create resource key for service instance: %s with resp code: %d", err.Error(), resp.StatusCode)
		}

		d.Config.ResourceKeyCRN = *key.CRN
		cr.Status.Storage.IBMCOS.ResourceKeyCRN = d.Config.ResourceKeyCRN
		cr.Spec.Storage.IBMCOS = d.Config.DeepCopy()
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "IBM COS Resource Key Creation Successful", "IBM COS resource key was successfully created")
	} else {
		// Get resource key
		key, resp, err := rc.GetResourceKeyWithContext(
			d.Context,
			&resourcecontrollerv2.GetResourceKeyOptions{
				ID: &d.Config.ResourceKeyCRN,
			},
		)
		if err != nil {
			return fmt.Errorf("unable to get resource key for service instance: %s with resp code: %d", err.Error(), resp.StatusCode)
		}

		// Check if resource key is for service instance
		if *key.SourceCRN != d.Config.ServiceInstanceCRN {
			return fmt.Errorf("specified resource key is not valid for service instance")
		}

		if key.Credentials != nil {
			// Check if resource key is HMAC enabled
			if key.Credentials.GetProperty("cos_hmac_keys") == nil {
				return fmt.Errorf("specified resource key credentials does not contain HMAC keys")
			}
			// Check if resource key has a valid IAM role
			if *key.Credentials.IamRoleCRN != "crn:v1:bluemix:public:iam::::serviceRole:Writer" && *key.Credentials.IamRoleCRN != "crn:v1:bluemix:public:iam::::serviceRole:Manager" {
				return fmt.Errorf("specified resource key's IAM role is not valid")
			}
			// Valid resource key
			d.Config.ResourceKeyCRN = *key.CRN
			cr.Status.Storage.IBMCOS.ResourceKeyCRN = d.Config.ResourceKeyCRN
			cr.Spec.Storage.IBMCOS = d.Config.DeepCopy()
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "IBM COS Resource Key Valid", "IBM COS resource key exists and is valid")
		} else {
			return fmt.Errorf("specified resource key does not have any attached credentials")
		}
	}

	// Check if bucket already exists
	var bucketExists bool
	if len(d.Config.Bucket) != 0 {
		if err := d.bucketExists(d.Config.Bucket, d.Config.ServiceInstanceCRN); err != nil {
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

	// Create new bucket if required
	if len(d.Config.Bucket) != 0 && bucketExists {
		// Bucket exists
		if cr.Spec.Storage.ManagementState == "" {
			cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateUnmanaged
		}

		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			IBMCOS: d.Config.DeepCopy(),
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "IBM COS Bucket Exists", "User supplied IBM COS bucket exists and is accessible")
	} else {
		// Attempt to create new bucket
		if len(d.Config.Bucket) == 0 {
			if d.Config.Bucket, err = util.GenerateStorageName(d.Listers, d.Config.Location); err != nil {
				return err
			}
		}

		// Get COS client
		client, err := d.getIBMCOSClient(d.Config.ServiceInstanceCRN)
		if err != nil {
			return err
		}

		// Create COS bucket
		_, err = client.CreateBucketWithContext(
			d.Context,
			&s3.CreateBucketInput{
				Bucket: aws.String(d.Config.Bucket),
				CreateBucketConfiguration: &s3.CreateBucketConfiguration{
					LocationConstraint: aws.String(fmt.Sprintf("%s-smart", d.Config.Location)),
				},
			},
		)
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			}
			return err
		}

		// Wait until the bucket exists
		if err := client.WaitUntilBucketExistsWithContext(
			d.Context,
			&s3.HeadBucketInput{
				Bucket: aws.String(d.Config.Bucket),
			},
		); err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, aerr.Code(), aerr.Error())
			}
			return err
		}

		if cr.Spec.Storage.ManagementState == "" {
			cr.Spec.Storage.ManagementState = imageregistryv1.StorageManagementStateManaged
		}
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			IBMCOS: d.Config.DeepCopy(),
		}
		cr.Spec.Storage.IBMCOS = d.Config.DeepCopy()
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Creation Successful", "IBM COS bucket was successfully created")
	}

	return nil
}

// setServiceEndpointOverrides will collect any necessary IBM Cloud Service endpoint overrides and set them for the driver to use for IBM Cloud Services
func (d *driver) setServiceEndpointOverrides(infra *configapiv1.Infrastructure) {
	// We currently only handle overrides for IBMCloud (api/config/v1/IBMCloudPlatformType), not PowerVS
	if infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.Type == configapiv1.IBMCloudPlatformType && infra.Status.PlatformStatus.IBMCloud != nil {
		if len(infra.Status.PlatformStatus.IBMCloud.ServiceEndpoints) > 0 {
			for _, endpoint := range infra.Status.PlatformStatus.IBMCloud.ServiceEndpoints {
				switch endpoint.Name {
				case configapiv1.IBMCloudServiceCOS:
					klog.Infof("found override for ibmcloud cos endpoint: %s", endpoint.URL)
					d.cosServiceEndpoint = endpoint.URL
				case configapiv1.IBMCloudServiceIAM:
					klog.Infof("found override for ibmcloud iam endpoint: %s", endpoint.URL)
					d.iamServiceEndpoint = endpoint.URL
				case configapiv1.IBMCloudServiceResourceController:
					klog.Infof("found override for ibmcloud resource controller endpoint: %s", endpoint.URL)
					d.rcServiceEndpoint = endpoint.URL
				case configapiv1.IBMCloudServiceResourceManager:
					klog.Infof("found override for ibmcloud resource manager endpoint: %s", endpoint.URL)
					d.rmServiceEndpoint = endpoint.URL
				case configapiv1.IBMCloudServiceCIS, configapiv1.IBMCloudServiceDNSServices, configapiv1.IBMCloudServiceGlobalSearch, configapiv1.IBMCloudServiceGlobalTagging, configapiv1.IBMCloudServiceHyperProtect, configapiv1.IBMCloudServiceKeyProtect, configapiv1.IBMCloudServiceVPC:
					klog.Infof("ignoring unused service endpoint: %s", endpoint.Name)
				default:
					klog.Infof("ignoring unknown service: %s", endpoint.Name)
				}
			}
		}
	}
}

// getAccountID returns the IBM Cloud account ID associated with the
// IAM API key.
func (d *driver) getAccountID() (string, error) {
	IAMAPIKey, err := d.getCredentialsConfigData()
	if err != nil {
		return "", err
	}

	iamAuthenticator := &core.IamAuthenticator{
		ApiKey: IAMAPIKey,
	}

	if d.iamServiceEndpoint != "" {
		iamAuthenticator.URL = d.iamServiceEndpoint
	}

	// Get IAM token
	iamToken, err := iamAuthenticator.RequestToken()
	if err != nil {
		return "", err
	}
	parsedToken, _ := jwt.Parse(iamToken.AccessToken, nil)

	// Get account ID
	var accountID string
	if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok {
		if accountInfo, ok := claims["account"].(map[string]interface{}); ok {
			if accountInfo["bss"] != nil {
				accountID = accountInfo["bss"].(string)
			}
		}
	}

	if accountID == "" {
		return "", fmt.Errorf("could not parse account id from token")
	}

	return accountID, nil
}

// getResourceControllerService returns the IBM Cloud resource controller client.
func (d *driver) getResourceControllerService() (*resourcecontrollerv2.ResourceControllerV2, error) {
	if d.resourceController != nil {
		return d.resourceController, nil
	}

	// Fetch the latest Infrastructure Status, for any endpoint changes
	infra, err := util.GetInfrastructure(d.Listers.Infrastructures)
	if err != nil {
		return nil, err
	}
	d.setServiceEndpointOverrides(infra)

	IAMAPIKey, err := d.getCredentialsConfigData()
	if err != nil {
		return nil, err
	}

	authenticator := &core.IamAuthenticator{
		ApiKey: IAMAPIKey,
	}

	if d.iamServiceEndpoint != "" {
		authenticator.URL = d.iamServiceEndpoint
	}

	rcOptions := &resourcecontrollerv2.ResourceControllerV2Options{
		Authenticator: authenticator,
	}
	if d.rcServiceEndpoint != "" {
		rcOptions.URL = d.rcServiceEndpoint
	}

	service, err := resourcecontrollerv2.NewResourceControllerV2(rcOptions)
	if err != nil {
		return nil, err
	}

	return service, nil
}

// getResourceManagerService returns the IBM Cloud resource manager client.
func (d *driver) getResourceManagerService() (*resourcemanagerv2.ResourceManagerV2, error) {
	if d.resourceManager != nil {
		return d.resourceManager, nil
	}

	// Fetch the latest Infrastructure Status, for any endpoint changes
	infra, err := util.GetInfrastructure(d.Listers.Infrastructures)
	if err != nil {
		return nil, err
	}
	d.setServiceEndpointOverrides(infra)

	IAMAPIKey, err := d.getCredentialsConfigData()
	if err != nil {
		return nil, err
	}

	authenticator := &core.IamAuthenticator{
		ApiKey: IAMAPIKey,
	}

	if d.iamServiceEndpoint != "" {
		authenticator.URL = d.iamServiceEndpoint
	}

	rmOptions := &resourcemanagerv2.ResourceManagerV2Options{
		Authenticator: authenticator,
	}
	if d.rmServiceEndpoint != "" {
		rmOptions.URL = d.rmServiceEndpoint
	}

	service, err := resourcemanagerv2.NewResourceManagerV2(rmOptions)
	if err != nil {
		return nil, err
	}

	return service, nil
}

// ID returns the underlying storage identifier, in this case the bucket name.
func (d *driver) ID() string {
	return d.Config.Bucket
}

// RemoveStorage deletes the storage medium that was created.
// The COS bucket must be empty before it can be removed.
func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (bool, error) {
	// Not enough info for clean up
	if len(d.Config.Bucket) == 0 || len(d.Config.ServiceInstanceCRN) == 0 {
		return false, nil
	}

	// Only clean up if managed
	if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged {
		return false, nil
	}

	client, err := d.getIBMCOSClient(d.Config.ServiceInstanceCRN)
	if err != nil {
		return false, err
	}

	iter := s3manager.NewDeleteListIterator(client, &s3.ListObjectsInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	err = s3manager.NewBatchDeleteWithClient(client).Delete(d.Context, iter)
	if err != nil && !isBucketNotFound(err) {
		return false, err
	}

	_, err = client.DeleteBucketWithContext(d.Context, &s3.DeleteBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchBucket {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "IBM COS Bucket Deleted", "IBM COS bucket did not exist.")
				return false, nil
			}
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, aerr.Code(), aerr.Error())
			return false, err
		}
		return true, err
	}

	// Wait until the bucket does not exist
	if err := client.WaitUntilBucketNotExistsWithContext(d.Context, &s3.HeadBucketInput{
		Bucket: aws.String(d.Config.Bucket),
	}); err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, aerr.Code(), aerr.Error())
		}
		return false, err
	}

	if len(cr.Spec.Storage.IBMCOS.Bucket) != 0 {
		cr.Spec.Storage.IBMCOS.Bucket = ""
	}

	d.Config.Bucket = ""

	if !reflect.DeepEqual(cr.Status.Storage.IBMCOS, d.Config) {
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			IBMCOS: d.Config.DeepCopy(),
		}
	}

	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "IBM COS Bucket Deleted", "IBM COS bucket has been removed.")

	return false, nil
}

// isBucketNotFound determines if a set of S3 errors are indicative
// of if a bucket is truly not found.
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

// StorageChanged checks to see if the name of the storage medium
// has changed.
func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.IBMCOS, cr.Spec.Storage.IBMCOS) {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "IBMCOS Configuration Changed", "IBMCOS storage is in an unknown state")
		return true
	}
	return false
}

// StorageExists checks if an IBM COS bucket with the given name exists
// and we can access it.
func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	if len(d.Config.Bucket) == 0 || len(d.Config.ServiceInstanceCRN) == 0 {
		return false, nil
	}

	err := d.bucketExists(d.Config.Bucket, d.Config.ServiceInstanceCRN)
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

	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "IBM COS Bucket Exists", "")
	return true, nil
}

// bucketExists checks whether or not the IBM COS bucket exists.
func (d *driver) bucketExists(bucketName string, serviceInstanceCRN string) error {
	client, err := d.getIBMCOSClient(serviceInstanceCRN)
	if err != nil {
		return err
	}

	_, err = client.HeadBucketWithContext(
		d.Context,
		&s3.HeadBucketInput{
			Bucket: &bucketName,
		},
	)

	return err
}

// getIBMCOSClient returns a client that allows us to interact
// with the IBM COS service.
func (d *driver) getIBMCOSClient(serviceInstanceCRN string) (*s3.S3, error) {
	// Fetch the latest Infrastructure Status, for any endpoint changes
	infra, err := util.GetInfrastructure(d.Listers.Infrastructures)
	if err != nil {
		return nil, err
	}

	IBMCOSLocation := imageregistryv1.ImageRegistryConfigStorageIBMCOS{}.Location
	if infra.Status.PlatformStatus != nil {
		if infra.Status.PlatformStatus.Type == configapiv1.IBMCloudPlatformType && infra.Status.PlatformStatus.IBMCloud != nil {
			IBMCOSLocation = infra.Status.PlatformStatus.IBMCloud.Location
		}
		if infra.Status.PlatformStatus.Type == configapiv1.PowerVSPlatformType && infra.Status.PlatformStatus.PowerVS != nil {
			IBMCOSLocation, err = powerUtils.COSRegionForPowerVSRegion(infra.Status.PlatformStatus.PowerVS.Region)
			if err != nil {
				return nil, err
			}
		}
	}
	d.setServiceEndpointOverrides(infra)

	if IBMCOSLocation == "" {
		return nil, fmt.Errorf("unable to get location from infrastructure")
	}

	cosServiceEndpoint := fmt.Sprintf(cosEndpointTemplate, IBMCOSLocation)
	iamTokenEndpoint := IAMEndpoint
	if d.cosServiceEndpoint != "" {
		cosServiceEndpoint = d.cosServiceEndpoint
	}
	if d.iamServiceEndpoint != "" {
		iamTokenEndpoint = fmt.Sprintf("%s%s", d.iamServiceEndpoint, IAMTokenPath)
	}

	IAMAPIKey, err := d.getCredentialsConfigData()
	if err != nil {
		return nil, err
	}

	awsOptions := session.Options{
		Config: aws.Config{
			Endpoint: &cosServiceEndpoint,
			Region:   &d.Config.Location,
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
			S3ForcePathStyle: aws.Bool(true),
		},
	}

	if d.roundTripper != nil {
		awsOptions.Config.Credentials = credentials.AnonymousCredentials
		awsOptions.Config.HTTPClient.Transport = d.roundTripper
	} else {
		awsOptions.Config.Credentials = ibmiam.NewStaticCredentials(aws.NewConfig(), iamTokenEndpoint, IAMAPIKey, serviceInstanceCRN)
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

// getCredentialsConfigData reads credential data for IBM Cloud.
func (d *driver) getCredentialsConfigData() (string, error) {
	// Look for a user defined secret to get the IBM Cloud credentials from first
	sec, err := d.Listers.Secrets.Get(defaults.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
		// Fall back to those provided by the credential minter if nothing is provided by the user
		sec, err = d.Listers.Secrets.Get(defaults.CloudCredentialsName)
		if err != nil {
			return "", fmt.Errorf("unable to get cluster minted credentials %q: %v", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.CloudCredentialsName), err)
		}
		if v, ok := sec.Data["ibmcloud_api_key"]; ok {
			return string(v), nil
		} else {
			return "", fmt.Errorf("secret %q does not contain required key \"ibmcloud_api_key\"", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.CloudCredentialsName))
		}
	} else if err != nil {
		return "", err
	} else {
		if v, ok := sec.Data["REGISTRY_STORAGE_IBMCOS_IAMAPIKEY"]; ok {
			return string(v), nil
		} else {
			return "", fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_IBMCOS_IAMAPIKEY\"", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser))
		}
	}
}

// VolumeSecrets fetches HMAC credentials from a resource key and returns
// the credentials data so that it can be stored in the image-registry Pod's Secret.
func (d *driver) VolumeSecrets() (map[string]string, error) {
	if len(d.Config.ResourceKeyCRN) == 0 {
		return nil, fmt.Errorf("resource key has not been set")
	}

	// Get resource controller service
	rc, err := d.getResourceControllerService()
	if err != nil {
		return nil, err
	}

	// Get resource key
	key, resp, err := rc.GetResourceKeyWithContext(
		d.Context,
		&resourcecontrollerv2.GetResourceKeyOptions{
			ID: &d.Config.ResourceKeyCRN,
		},
	)
	if err != nil {
		respMsg := ""
		if resp != nil {
			respMsg = fmt.Sprintf(" with resp code: %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("unable to get resource key for service instance: %s%s", err.Error(), respMsg)
	}

	var accessKey string
	var accessSecret string

	if key.Credentials != nil {
		if prop := key.Credentials.GetProperty("cos_hmac_keys"); prop != nil {
			if hmacKeys, ok := prop.(map[string]interface{}); ok {
				accessKey = hmacKeys["access_key_id"].(string)
				accessSecret = hmacKeys["secret_access_key"].(string)
			} else {
				return nil, fmt.Errorf("unable to convert data for HMAC keys")
			}
		} else {
			return nil, fmt.Errorf("specified resource key credentials does not contain HMAC keys")
		}
	} else {
		return nil, fmt.Errorf("specified resource key does not have any attached credentials")
	}

	if accessKey == "" || accessSecret == "" {
		return nil, fmt.Errorf("unknown error occurred setting HMAC credentials")
	}

	buf := &bytes.Buffer{}
	fmt.Fprint(buf, "[default]\n")
	fmt.Fprintf(buf, "aws_access_key_id = %s\n", accessKey)
	fmt.Fprintf(buf, "aws_secret_access_key = %s\n", accessSecret)

	return map[string]string{
		imageRegistrySecretDataKey: buf.String(),
	}, nil
}

// Volumes returns configuration for mounting credentials data as a Volume for
// image-registry Pods.
func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	optional := false

	volume := corev1.Volume{
		Name: defaults.ImageRegistryPrivateConfiguration,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: defaults.ImageRegistryPrivateConfiguration,
				Optional:   &optional,
			},
		},
	}

	mount := corev1.VolumeMount{
		Name:      volume.Name,
		MountPath: imageRegistrySecretMountpoint,
		ReadOnly:  true,
	}

	return []corev1.Volume{volume}, []corev1.VolumeMount{mount}, nil
}
