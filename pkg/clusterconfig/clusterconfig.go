package clusterconfig

import (
	"fmt"
	"strings"
	"time"

	"github.com/gophercloud/utils/openstack/clientconfig"
	yamlv2 "gopkg.in/yaml.v2"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	installer "github.com/openshift/installer/pkg/types"
)

const (
	installerConfigNamespace = "kube-system"
	installerConfigName      = "cluster-config-v1"
	cloudCredentialsName     = "installer-cloud-credentials"
)

type StorageType string

type Azure struct {
	AccountName string
	AccountKey  string
	Container   string
}

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
	AuthURL    string
	Username   string
	Password   string
	Tenant     string
	TenantID   string
	Domain     string
	DomainID   string
	RegionName string
}

type Storage struct {
	Azure Azure
	GCS   GCS
	S3    S3
	Swift Swift
}

type Config struct {
	Storage Storage
}

func GetCoreClient() (*coreset.CoreV1Client, error) {
	kubeconfig, err := regopclient.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := coreset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func GetInstallConfig() (*installer.InstallConfig, error) {
	client, err := GetCoreClient()
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

func GetAWSConfig(listers *regopclient.Listers) (*Config, error) {
	cfg := &Config{}

	installConfig, err := GetInstallConfig()
	if err != nil {
		return nil, err
	}

	if installConfig.Platform.AWS != nil {
		cfg.Storage.S3.Region = installConfig.Platform.AWS.Region
	}

	client, err := GetCoreClient()
	if err != nil {
		return nil, err
	}

	// Look for a user defined secret to get the AWS credentials from first
	sec, err := listers.Secrets.Get(imageregistryv1.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
		pollErr := wait.PollImmediate(1*time.Second, 5*time.Minute, func() (stop bool, err error) {
			sec, err = client.Secrets(imageregistryv1.ImageRegistryOperatorNamespace).Get(cloudCredentialsName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				} else {
					return false, err
				}
			}
			return true, nil
		})

		if sec == nil || pollErr != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials %q: %v", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, cloudCredentialsName), pollErr)
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
		if v, ok := sec.Data["STORAGE_GCS_KEYFILE"]; ok {
			cfg.Storage.GCS.KeyfileData = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"STORAGE_GCS_KEYFILE\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, imageregistryv1.ImageRegistryPrivateConfigurationUser))
		}
	}

	return cfg, nil
}

// GetSwiftConfig reads credentials
func GetSwiftConfig(listers *regopclient.Listers) (*Config, error) {
	cfg := &Config{}

	// Look for a user defined secret to get the Swift credentials
	sec, err := listers.Secrets.Get(imageregistryv1.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
		// If no user defined credentials were provided, then try to find them in the secret,
		// created by cloud-credential-operator.
		pollErr := wait.PollImmediate(1*time.Second, 5*time.Minute, func() (stop bool, err error) {
			sec, err = listers.Secrets.Get(cloudCredentialsName)
			if err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		})

		if sec == nil || pollErr != nil {
			return nil, fmt.Errorf("unable to get cluster minted credentials %q: %v", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, cloudCredentialsName), pollErr)
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
			} else {
				return nil, fmt.Errorf("clouds.yaml does not contain required cloud \"openstack\"")
			}
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"clouds.yaml\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, cloudCredentialsName))
		}
	} else if err != nil {
		return nil, err
	} else {
		if v, ok := sec.Data["REGISTRY_STORAGE_SWIFT_USERNAME"]; ok {
			cfg.Storage.Swift.Username = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_SWIFT_USERNAME\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, imageregistryv1.ImageRegistryPrivateConfigurationUser))
		}
		if v, ok := sec.Data["REGISTRY_STORAGE_SWIFT_PASSWORD"]; ok {
			cfg.Storage.Swift.Password = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_SWIFT_PASSWORD\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, imageregistryv1.ImageRegistryPrivateConfigurationUser))

		}
	}

	return cfg, nil
}
