package clusterconfig

import (
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcore "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
)

var (
	// hardcode it for now
	configName      string = "global"
	configNamespace string = "openshift-image-registry"
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
	Type  string
	Azure Azure
	GCS   GCS
	S3    S3
}

type Config struct {
	Storage Storage
}

func Get() (*Config, error) {
	kubeconfig, err := restclient.InClusterConfig()
	if err != nil {
		return nil, err
	}

	client, err := clientcore.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	cm, err := client.ConfigMaps(configNamespace).Get(configName, metaapi.GetOptions{})
	if err != nil {
		return nil, err
	}

	cfg := &Config{}

	if v, ok := cm.Data["storage_type"]; ok {
		cfg.Storage.Type = v
	}

	mapkeys := map[string]*string{
		"storage_azure_accountname": &cfg.Storage.Azure.AccountName,
		"storage_azure_accountkey":  &cfg.Storage.Azure.AccountKey,
		"storage_azure_container":   &cfg.Storage.Azure.Container,
		"storage_gcs_bucket":        &cfg.Storage.GCS.Bucket,
		"storage_gcs_keyfile":       &cfg.Storage.GCS.KeyfileData,
		"storage_aws_accesskey":     &cfg.Storage.S3.AccessKey,
		"storage_aws_secretkey":     &cfg.Storage.S3.SecretKey,
		"storage_aws_region":        &cfg.Storage.S3.Region,
		"storage_aws_bucket":        &cfg.Storage.S3.Bucket,
	}

	for param, dest := range mapkeys {
		if v, ok := cm.Data[param]; ok {
			*dest = v
		}
	}

	return cfg, nil
}
