package storage

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	configv1 "github.com/openshift/api/config/v1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/emptydir"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/pvc"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
)

var (
	ErrStorageNotConfigured = fmt.Errorf("storage backend not configured")
)

type Driver interface {
	ConfigEnv() ([]corev1.EnvVar, error)
	Volumes() ([]corev1.Volume, []corev1.VolumeMount, error)
	Secrets() (map[string]string, error)
	CompleteConfiguration(*imageregistryv1.Config) error
	CreateStorage(*imageregistryv1.Config) error
	StorageExists(*imageregistryv1.Config) (bool, error)
	RemoveStorage(*imageregistryv1.Config) (bool, error)
	StorageChanged(*imageregistryv1.Config) bool
}

func newDriver(cfg *imageregistryv1.ImageRegistryConfigStorage, listers *regopclient.Listers) (Driver, error) {
	var names []string
	var drivers []Driver

	if cfg.EmptyDir != nil {
		names = append(names, "EmptyDir")
		drivers = append(drivers, emptydir.NewDriver(cfg.EmptyDir, listers))
	}

	if cfg.S3 != nil {
		names = append(names, "S3")
		drivers = append(drivers, s3.NewDriver(cfg.S3, listers))
	}

	if cfg.PVC != nil {
		drv, err := pvc.NewDriver(cfg.PVC)
		if err != nil {
			return nil, err
		}
		names = append(names, "PVC")
		drivers = append(drivers, drv)
	}

	switch len(drivers) {
	case 0:
		return nil, ErrStorageNotConfigured
	case 1:
		return drivers[0], nil
	}

	return nil, fmt.Errorf("exactly one storage type should be configured at the same time, got %d: %v", len(drivers), names)
}

func NewDriver(cfg *imageregistryv1.ImageRegistryConfigStorage, listers *regopclient.Listers) (Driver, error) {
	drv, err := newDriver(cfg, listers)
	if err == ErrStorageNotConfigured {
		*cfg, err = getPlatformStorage()
		if err != nil {
			return nil, fmt.Errorf("unable to get storage configuration from cluster install config: %s", err)
		}
		drv, err = newDriver(cfg, listers)
	}
	return drv, err
}

// getPlatformStorage returns the storage configuration that should be used
// based on the cloudplatform we are running on, as determined from the
// installer configuration.
func getPlatformStorage() (imageregistryv1.ImageRegistryConfigStorage, error) {
	var cfg imageregistryv1.ImageRegistryConfigStorage

	config, err := regopclient.GetConfig()
	if err != nil {
		return cfg, err
	}

	client, err := clientcorev1.NewForConfig(config)
	if err != nil {
		return cfg, err
	}

	infra, err := osclientset.New(client.RESTClient()).ConfigV1().Infrastructures().Get("cluster", metav1.GetOptions{})
	if err != nil {
		return cfg, err
	}

	switch {
	case infra.Status.Platform == configv1.LibvirtPlatformType:
		cfg.EmptyDir = &imageregistryv1.ImageRegistryConfigStorageEmptyDir{}
	case infra.Status.Platform == configv1.AWSPlatformType:
		cfg.S3 = &imageregistryv1.ImageRegistryConfigStorageS3{}
	case infra.Status.Platform == configv1.OpenStackPlatformType:
		// TODO(flaper87): This should be switch to swift as soon as support for
		// it is complete. Using Emptydir for now so that OpenStack deployments
		// (and work) can move forward for now. Not production ready!
		cfg.EmptyDir = &imageregistryv1.ImageRegistryConfigStorageEmptyDir{}
	}

	return cfg, nil
}
