package util

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	installer "github.com/openshift/installer/pkg/types"
)

type StorageType string

const (
	STORAGE_PREFIX = "image-registry"

	StorageTypeAzure StorageType = "azure"

	StorageTypeGCS StorageType = "gcs"

	StorageTypeS3 StorageType = "s3"

	StorageTypeEmptyDir StorageType = "emptydir"

	StorageTypeFileSystem StorageType = "filesystem"

	StorageTypeSwift StorageType = "swift"
)

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

func GetInstallConfig() (*installer.InstallConfig, error) {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-config-v1",
			Namespace: "kube-system",
		},
	}

	if err := sdk.Get(cm); err != nil {
		return nil, fmt.Errorf("unable to read cluster install configuration: %v", err)
	}

	installConfig := &installer.InstallConfig{}
	if err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(cm.Data["install-config"]), 100).Decode(installConfig); err != nil {
		return nil, fmt.Errorf("unable to decode cluster install configuration: %v", err)
	}

	return installConfig, nil
}

func GetAWSConfig() (*Config, error) {
	cfg := &Config{}

	installConfig, err := GetInstallConfig()
	if err != nil {
		return nil, err
	}
	cfg.Storage.Type = StorageTypeS3
	cfg.Storage.S3.Region = installConfig.Platform.AWS.Region

	sec := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aws-creds",
			Namespace: "kube-system",
		},
	}

	if err := sdk.Get(sec); err != nil {
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
