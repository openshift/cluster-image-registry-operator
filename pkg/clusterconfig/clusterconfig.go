package clusterconfig

import (
	"fmt"
	"strings"

	installer "github.com/openshift/installer/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

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
	sec, err := client.Secrets(installerConfigNamespace).Get(installerAWSCredsName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to read aws-creds secret: %v", err)
	}

	cfg.Storage.Type = StorageTypeS3
	if v, ok := sec.Data["aws_access_key_id"]; ok {
		cfg.Storage.S3.AccessKey = string(v)
	}
	if v, ok := sec.Data["aws_secret_access_key"]; ok {
		cfg.Storage.S3.SecretKey = string(v)
	}

	return cfg, nil
}

func GetGCSConfig() (*Config, error) {
	cfg := &Config{}
	cfg.Storage.Type = StorageTypeGCS
	return cfg, nil
}
