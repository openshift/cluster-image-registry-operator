package clusterconfig

import (
	"fmt"
	"strings"

	"github.com/golang/glog"

	installer "github.com/openshift/installer/pkg/types"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
)

const (
	StoragePrefix = "image-registry"

	StorageTypeAzure      StorageType = "azure"
	StorageTypeGCS        StorageType = "gcs"
	StorageTypeS3         StorageType = "s3"
	StorageTypeEmptyDir   StorageType = "emptydir"
	StorageTypeFileSystem StorageType = "filesystem"
	StorageTypeSwift      StorageType = "swift"

	installerConfigNamespace = "kube-system"
	installerConfigName      = "cluster-config-v1"
	installerAWSCredsName    = "aws-creds"
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

type Storage struct {
	Type  StorageType
	Azure Azure
	GCS   GCS
	S3    S3
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

func GetAWSConfig() (*Config, error) {
	client, err := GetCoreClient()
	if err != nil {
		return nil, err
	}
	cfg := &Config{}

	installConfig, err := GetInstallConfig()
	if err != nil {
		return nil, err
	}

	cfg.Storage.Type = StorageTypeS3
	if installConfig.Platform.AWS != nil {
		cfg.Storage.S3.Region = installConfig.Platform.AWS.Region
	}

	// Look for a user defined secret to get the AWS credentials from first
	sec, err := client.Secrets(regopapi.OperatorNamespace).Get(regopapi.ImageRegistryPrivateConfigurationUser, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		glog.Infof("Optional user defined AWS credentials in secret \"%s/%s\" not found, ignoring.", regopapi.OperatorNamespace, regopapi.ImageRegistryPrivateConfigurationUser)
		// If no user defined secret is found, use the system one
		sec, err = client.Secrets(installerConfigNamespace).Get(installerAWSCredsName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("unable to get secret %q: %v", fmt.Sprintf("%s/%s", installerConfigNamespace, installerAWSCredsName), err)
		}
		if v, ok := sec.Data["aws_access_key_id"]; ok {
			cfg.Storage.S3.AccessKey = string(v)
		} else {
			return nil, fmt.Errorf("Secret %q does not contain required key \"aws_access_key_id\"", fmt.Sprintf("%s/%s", installerConfigNamespace, installerAWSCredsName))
		}
		if v, ok := sec.Data["aws_secret_access_key"]; ok {
			cfg.Storage.S3.SecretKey = string(v)
		} else {
			return nil, fmt.Errorf("Secret %q does not contain required key \"aws_secret_access_key\"", fmt.Sprintf("%s/%s", installerConfigNamespace, installerAWSCredsName))
		}
	} else if err != nil {
		return nil, err
	} else {
		if v, ok := sec.Data["REGISTRY_STORAGE_S3_ACCESSKEY"]; ok {
			cfg.Storage.S3.AccessKey = string(v)
		} else {
			return nil, fmt.Errorf("Secret %q does not contain required key \"REGISTRY_STORAGE_S3_ACCESSKEY\"", fmt.Sprintf("%s/%s", regopapi.OperatorNamespace, regopapi.ImageRegistryPrivateConfigurationUser))
		}
		if v, ok := sec.Data["REGISTRY_STORAGE_S3_SECRETKEY"]; ok {
			cfg.Storage.S3.SecretKey = string(v)
		} else {
			return nil, fmt.Errorf("Secret %q does not contain required key \"REGISTRY_STORAGE_S3_SECRETKEY\"", fmt.Sprintf("%s/%s", regopapi.OperatorNamespace, regopapi.ImageRegistryPrivateConfigurationUser))

		}
	}

	return cfg, nil
}

func GetGCSConfig() (*Config, error) {
	cfg := &Config{}
	cfg.Storage.Type = StorageTypeGCS
	return cfg, nil
}
