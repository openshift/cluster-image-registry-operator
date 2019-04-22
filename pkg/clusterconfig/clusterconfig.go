package clusterconfig

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	configv1 "github.com/openshift/api/config/v1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
)

const (
	cloudCredentialsName = "installer-cloud-credentials"
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
	Azure Azure
	GCS   GCS
	S3    S3
}

type Config struct {
	Storage Storage
}

func getCoreClient() (*coreset.CoreV1Client, error) {
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

func GetAWSConfig(ctx context.Context, client rest.Interface, listers *regopclient.Listers) (*Config, error) {
	cfg := &Config{}

	infra, err := osclientset.New(client).ConfigV1().Infrastructures().Get("cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	if infra.Status.Platform == configv1.AWSPlatformType {
		cfg.Storage.S3.Region = "us-east-1" // FIXME: installConfig.Platform.AWS.Region
	}

	// Look for a user defined secret to get the AWS credentials from first
	sec, err := listers.Secrets.Get(imageregistryv1.ImageRegistryPrivateConfigurationUser)
	if err != nil && errors.IsNotFound(err) {
		pollErr := wait.PollImmediate(1*time.Second, 5*time.Minute, func() (stop bool, err error) {
			sec, err = coreset.New(client).Secrets(imageregistryv1.ImageRegistryOperatorNamespace).Get(cloudCredentialsName, metav1.GetOptions{})
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

func GetGCSConfig() (*Config, error) {
	cfg := &Config{}
	return cfg, nil
}
