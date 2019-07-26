package storage

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/azure"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/emptydir"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/gcs"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/pvc"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/swift"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

var (
	ErrStorageNotConfigured = fmt.Errorf("storage backend not configured")
)

type Driver interface {
	ConfigEnv() ([]corev1.EnvVar, error)
	Volumes() ([]corev1.Volume, []corev1.VolumeMount, error)
	Secrets() (map[string]string, error)
	CreateStorage(*imageregistryv1.Config) error
	StorageExists(*imageregistryv1.Config) (bool, error)
	RemoveStorage(*imageregistryv1.Config) (bool, error)
	StorageChanged(*imageregistryv1.Config) bool
}

func newDriver(cfg *imageregistryv1.ImageRegistryConfigStorage, kubeconfig *rest.Config, listers *regopclient.Listers) (Driver, error) {
	var names []string
	var drivers []Driver

	if cfg.EmptyDir != nil {
		names = append(names, "EmptyDir")
		drivers = append(drivers, emptydir.NewDriver(cfg.EmptyDir, listers))
	}

	if cfg.S3 != nil {
		names = append(names, "S3")
		drivers = append(drivers, s3.NewDriver(cfg.S3, kubeconfig, listers))
	}

	if cfg.Swift != nil {
		names = append(names, "Swift")
		drivers = append(drivers, swift.NewDriver(cfg.Swift, listers))
	}

	if cfg.GCS != nil {
		names = append(names, "GCS")
		ctx := context.Background()
		drivers = append(drivers, gcs.NewDriver(cfg.GCS, ctx, kubeconfig, listers))
	}

	if cfg.PVC != nil {
		drv, err := pvc.NewDriver(cfg.PVC, kubeconfig)
		if err != nil {
			return nil, err
		}
		names = append(names, "PVC")
		drivers = append(drivers, drv)
	}

	if cfg.Azure != nil {
		names = append(names, "Azure")
		drivers = append(drivers, azure.NewDriver(cfg.Azure, kubeconfig, listers))
	}

	switch len(drivers) {
	case 0:
		return nil, ErrStorageNotConfigured
	case 1:
		return drivers[0], nil
	}

	return nil, fmt.Errorf("exactly one storage type should be configured at the same time, got %d: %v", len(drivers), names)
}

func NewDriver(cfg *imageregistryv1.ImageRegistryConfigStorage, kubeconfig *rest.Config, listers *regopclient.Listers) (Driver, error) {
	drv, err := newDriver(cfg, kubeconfig, listers)
	if err == ErrStorageNotConfigured {
		*cfg, err = getPlatformStorage(listers)
		if err != nil {
			return nil, fmt.Errorf("unable to get storage configuration from cluster install config: %s", err)
		}
		drv, err = newDriver(cfg, kubeconfig, listers)
	}
	return drv, err
}

// getPlatformStorage returns the storage configuration that should be used
// based on the cloudplatform we are running on, as determined from the
// infrastructure configuration.
func getPlatformStorage(listers *regopclient.Listers) (imageregistryv1.ImageRegistryConfigStorage, error) {
	var cfg imageregistryv1.ImageRegistryConfigStorage

	infra, err := util.GetInfrastructure(listers)
	if err != nil {
		return imageregistryv1.ImageRegistryConfigStorage{}, err
	}

	switch infra.Status.PlatformStatus.Type {
	case configapiv1.LibvirtPlatformType:
		cfg.EmptyDir = &imageregistryv1.ImageRegistryConfigStorageEmptyDir{}
	case configapiv1.BareMetalPlatformType:
		// There is no specific known storage type available for a "baremetal"
		// platform deployment at install time, so we default to EmptyDir to
		// allow the installation to complete cleanly.  This must be
		// re-configured to use a PVC post-install once a storage platform has
		// been configured.  Note that the only supported use of the
		// "baremetal" platform does include rook/ceph based storage, so
		// EmptyDir will never be used in a production cluster.
		cfg.EmptyDir = &imageregistryv1.ImageRegistryConfigStorageEmptyDir{}
	case configapiv1.AWSPlatformType:
		cfg.S3 = &imageregistryv1.ImageRegistryConfigStorageS3{}
	case configapiv1.AzurePlatformType:
		cfg.Azure = &imageregistryv1.ImageRegistryConfigStorageAzure{}
	case configapiv1.GCPPlatformType:
		cfg.GCS = &imageregistryv1.ImageRegistryConfigStorageGCS{}
	case configapiv1.OpenStackPlatformType:
		cfg.Swift = &imageregistryv1.ImageRegistryConfigStorageSwift{}
	}

	return cfg, nil
}
