package clusterconfig

import (
	"fmt"
	"strings"

	"github.com/gophercloud/utils/openstack/clientconfig"
	yamlv2 "gopkg.in/yaml.v2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"

	configapiv1 "github.com/openshift/api/config/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	installer "github.com/openshift/installer/pkg/types"
)

const (
	installerConfigNamespace = "kube-system"
	installerConfigName      = "cluster-config-v1"
	azureCredentialsName     = "azure-credentials"
	cloudCredentialsName     = "installer-cloud-credentials"
)

type StorageType string

type GCS struct {
	Bucket      string
	KeyfileData string
}

type S3 struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
}

type Swift struct {
	AuthURL            string
	Username           string
	Password           string
	Tenant             string
	TenantID           string
	Domain             string
	DomainID           string
	RegionName         string
	IdentityAPIVersion string
}

type Azure struct {
	// IPI
	SubscriptionID string
	ClientID       string
	ClientSecret   string
	TenantID       string
	ResourceGroup  string
	Region         string

	// UPI
	AccountKey string
}

type Storage struct {
	GCS   GCS
	S3    S3
	Swift Swift
	Azure Azure
}

type Config struct {
	Storage Storage
}

func GetCoreClient(kubeconfig *rest.Config) (*coreset.CoreV1Client, error) {
	client, err := coreset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func GetInstallConfig(kubeconfig *rest.Config) (*installer.InstallConfig, error) {
	client, err := GetCoreClient(kubeconfig)
	if err != nil {
		return nil, err
	}

	cm, err := client.ConfigMaps(installerConfigNamespace).Get(installerConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to read cluster install configuration: %v", err)
	}

	installConfig := &installer.InstallConfig{}
	if err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(cm.Data["install-config"]), 100).Decode(installConfig); err != nil {
		return nil, fmt.Errorf("unable to decode cluster install configuration: %v", err)
	}

	return installConfig, nil
}

func GetAWSConfig(kubeconfig *rest.Config, listers *regopclient.Listers) (*Config, error) {
	cfg := &Config{}

	infra, err := util.GetInfrastructure(kubeconfig)
	if err != nil {
		return nil, err
	}

	installConfig, err := GetInstallConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	platformType := infra.Status.Platform
	if infra.Status.PlatformStatus != nil {
		platformType = infra.Status.PlatformStatus.Type
	}
	if platformType == configapiv1.AWSPlatformType {
		AWSRegion := installConfig.Platform.AWS.Region
		if infra.Status.PlatformStatus != nil {
			AWSRegion = infra.Status.PlatformStatus.AWS.Region
		}
		cfg.Storage.S3.Region = AWSRegion
	}

	client, err := GetCoreClient(kubeconfig)
	if err != nil {
		return nil, err
	}

	// Look for a user defined secret to get the AWS credentials from first
	sec, err := listers.Secrets.Get(imageregistryv1.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
		sec, err = client.Secrets(imageregistryv1.ImageRegistryOperatorNamespace).Get(cloudCredentialsName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials %q: %v", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, cloudCredentialsName), err)
		}

		if v, ok := sec.Data["aws_access_key_id"]; ok {
			cfg.Storage.S3.AccessKey = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"aws_access_key_id\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, cloudCredentialsName))
		}
		if v, ok := sec.Data["aws_secret_access_key"]; ok {
			cfg.Storage.S3.SecretKey = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"aws_secret_access_key\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, cloudCredentialsName))
		}
	} else if err != nil {
		return nil, err
	} else {
		if v, ok := sec.Data["REGISTRY_STORAGE_S3_ACCESSKEY"]; ok {
			cfg.Storage.S3.AccessKey = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_S3_ACCESSKEY\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, imageregistryv1.ImageRegistryPrivateConfigurationUser))
		}
		if v, ok := sec.Data["REGISTRY_STORAGE_S3_SECRETKEY"]; ok {
			cfg.Storage.S3.SecretKey = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_S3_SECRETKEY\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, imageregistryv1.ImageRegistryPrivateConfigurationUser))

		}
	}

	return cfg, nil
}

func GetGCSConfig(listers *regopclient.Listers) (*Config, error) {
	cfg := &Config{}

	// Look for a user defined secret to get the GCS credentials from
	sec, err := listers.Secrets.Get(imageregistryv1.ImageRegistryPrivateConfigurationUser)
	if err != nil {
		return nil, err
	} else {
		// GCS credentials are stored in a file that can be downloaded from the
		// GCP console
		if v, ok := sec.Data["REGISTRY_STORAGE_GCS_KEYFILE"]; ok {
			cfg.Storage.GCS.KeyfileData = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_GCS_KEYFILE\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, imageregistryv1.ImageRegistryPrivateConfigurationUser))
		}
	}

	return cfg, nil
}

func getValueFromSecret(sec *corev1.Secret, key string) (string, error) {
	if v, ok := sec.Data[key]; ok {
		return string(v), nil
	}
	return "", fmt.Errorf("secret %q does not contain required key %q", fmt.Sprintf("%s/%s", sec.Namespace, sec.Name), key)
}

// GetSwiftConfig reads credentials
func GetSwiftConfig(listers *regopclient.Listers) (*Config, error) {
	cfg := &Config{}

	// Look for a user defined secret to get the Swift credentials
	sec, err := listers.Secrets.Get(imageregistryv1.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
		// If no user defined credentials were provided, then try to find them in the secret,
		// created by cloud-credential-operator.
		sec, err = listers.Secrets.Get(cloudCredentialsName)
		if err != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials %q: %v", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, cloudCredentialsName), err)
		}

		// cloud-credential-operator is responsible for generating the clouds.yaml file and placing it in the local cloud creds secret.
		if cloudsData, ok := sec.Data["clouds.yaml"]; ok {
			var clouds clientconfig.Clouds
			err = yamlv2.Unmarshal(cloudsData, &clouds)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal clouds credentials: %v", err)
			}

			if cloud, ok := clouds.Clouds["openstack"]; ok {
				cfg.Storage.Swift.AuthURL = cloud.AuthInfo.AuthURL
				cfg.Storage.Swift.Username = cloud.AuthInfo.Username
				cfg.Storage.Swift.Password = cloud.AuthInfo.Password
				cfg.Storage.Swift.Tenant = cloud.AuthInfo.ProjectName
				cfg.Storage.Swift.TenantID = cloud.AuthInfo.ProjectID
				cfg.Storage.Swift.Domain = cloud.AuthInfo.DomainName
				cfg.Storage.Swift.DomainID = cloud.AuthInfo.DomainID
				if cfg.Storage.Swift.Domain == "" {
					cfg.Storage.Swift.Domain = cloud.AuthInfo.UserDomainName
				}
				if cfg.Storage.Swift.DomainID == "" {
					cfg.Storage.Swift.DomainID = cloud.AuthInfo.UserDomainID
				}
				cfg.Storage.Swift.RegionName = cloud.RegionName
				cfg.Storage.Swift.IdentityAPIVersion = cloud.IdentityAPIVersion
			} else {
				return nil, fmt.Errorf("clouds.yaml does not contain required cloud \"openstack\"")
			}
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"clouds.yaml\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, cloudCredentialsName))
		}
	} else if err != nil {
		return nil, err
	} else {
		cfg.Storage.Swift.Username, err = getValueFromSecret(sec, "REGISTRY_STORAGE_SWIFT_USERNAME")
		if err != nil {
			return nil, err
		}
		cfg.Storage.Swift.Password, err = getValueFromSecret(sec, "REGISTRY_STORAGE_SWIFT_PASSWORD")
		if err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func getAzureConfigFromCloudSecret(creds *corev1.Secret) (*Azure, error) {
	cfg := &Azure{}
	cfg.SubscriptionID = string(creds.Data["azure_subscription_id"])
	cfg.ClientID = string(creds.Data["azure_client_id"])
	cfg.ClientSecret = string(creds.Data["azure_client_secret"])
	cfg.TenantID = string(creds.Data["azure_tenant_id"])
	cfg.ResourceGroup = string(creds.Data["azure_resourcegroup"])
	cfg.Region = string(creds.Data["azure_region"])
	return cfg, nil
}

func getAzureConfigFromUserSecret(sec *corev1.Secret) (*Azure, error) {
	cfg := &Azure{}
	var err error

	cfg.AccountKey, err = getValueFromSecret(sec, "REGISTRY_STORAGE_AZURE_ACCOUNTKEY")
	if err != nil {
		return nil, err
	}
	if cfg.AccountKey == "" {
		return nil, fmt.Errorf("the secret %s/%s has an empty value for REGISTRY_STORAGE_AZURE_ACCOUNTKEY; the secret should be removed so that the operator can use cluster-wide secrets or it should contain a valid storage account access key", sec.Namespace, sec.Name)
	}

	return cfg, nil
}

// GetAzureConfig reads configuration for the Azure cloud platform services.
func GetAzureConfig(listers *regopclient.Listers) (*Azure, error) {
	sec, err := listers.Secrets.Get(imageregistryv1.ImageRegistryPrivateConfigurationUser)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("unable to get user provided secrets: %s", err)
		}

		creds, err := listers.Secrets.Get(cloudCredentialsName)
		if err != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials: %s", err)
		}
		return getAzureConfigFromCloudSecret(creds)
	}
	return getAzureConfigFromUserSecret(sec)
}
