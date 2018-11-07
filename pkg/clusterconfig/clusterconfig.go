package clusterconfig

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcore "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

// GetConfig creates a *rest.Config for talking to a Kubernetes apiserver.
// Otherwise will assume running in cluster and use the cluster provided kubeconfig.
//
// Config precedence
//
// * KUBECONFIG environment variable pointing at a file
//
// * In-cluster config if running in cluster
//
// * $HOME/.kube/config if exists
func GetConfig() (*restclient.Config, error) {
	// If an env variable is specified with the config locaiton, use that
	if len(os.Getenv("KUBECONFIG")) > 0 {
		return clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	}
	// If no explicit location, try the in-cluster config
	if c, err := restclient.InClusterConfig(); err == nil {
		return c, nil
	}
	// If no in-cluster config, try the default location in the user's home directory
	if usr, err := user.Current(); err == nil {
		if c, err := clientcmd.BuildConfigFromFlags(
			"", filepath.Join(usr.HomeDir, ".kube", "config")); err == nil {
			return c, nil
		}
	}

	return nil, fmt.Errorf("could not locate a kubeconfig")
}

func Get() (*Config, error) {
	kubeconfig, err := GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := clientcore.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}

	cm, err := client.ConfigMaps(configNamespace).Get(configName, metaapi.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logrus.Warnf("config-map %s/%s not found", configNamespace, configName)
			return cfg, nil
		}
		return nil, err
	}

	mapkeys := map[string]*string{
		"storage_type":              &cfg.Storage.Type,
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
