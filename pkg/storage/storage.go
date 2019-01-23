package storage

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/azure"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/emptydir"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/filesystem"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/gcs"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/swift"
)

var (
	ErrStorageNotConfigured = fmt.Errorf("storage backend not configured")
)

type Driver interface {
	ConfigEnv() ([]corev1.EnvVar, error)
	Volumes() ([]corev1.Volume, []corev1.VolumeMount, error)
	Secrets() (map[string]string, error)
	CompleteConfiguration(*imageregistryv1.Config, *bool) error
	CreateStorage(*imageregistryv1.Config, *bool) error
	StorageExists(*imageregistryv1.Config, *bool) (bool, error)
	RemoveStorage(*imageregistryv1.Config, *bool) (bool, error)
	StorageChanged(*imageregistryv1.Config, *bool) bool
	GetStorageName() string
	UpdateFromStorage(cfg imageregistryv1.ImageRegistryConfigStorage)
}

func newDriver(cfg *imageregistryv1.ImageRegistryConfigStorage) (Driver, error) {
	var names []string
	var drivers []Driver

	if cfg.Azure != nil {
		names = append(names, "Azure")
		drivers = append(drivers, azure.NewDriver(cfg.Azure))
	}

	if cfg.Filesystem != nil && cfg.Filesystem.VolumeSource.EmptyDir == nil {
		names = append(names, "Filesystem")
		drivers = append(drivers, filesystem.NewDriver(cfg.Filesystem))
	}

	if cfg.Filesystem != nil && cfg.Filesystem.VolumeSource.EmptyDir != nil {
		names = append(names, "Filesystem:EmptyDir")
		drivers = append(drivers, emptydir.NewDriver(cfg.Filesystem))
	}

	if cfg.GCS != nil {
		names = append(names, "GCS")
		drivers = append(drivers, gcs.NewDriver(cfg.GCS))
	}

	if cfg.S3 != nil {
		names = append(names, "S3")
		drivers = append(drivers, s3.NewDriver(cfg.S3))
	}

	if cfg.Swift != nil {
		names = append(names, "Swift")
		drivers = append(drivers, swift.NewDriver(cfg.Swift))
	}

	switch len(drivers) {
	case 0:
		return nil, ErrStorageNotConfigured
	case 1:
		return drivers[0], nil
	}

	return nil, fmt.Errorf("exactly one storage type should be configured at the same time, got %d: %v", len(drivers), names)
}

func NewDriver(cfg *imageregistryv1.ImageRegistryConfigStorage) (Driver, error) {
	drv, err := newDriver(cfg)
	if err == ErrStorageNotConfigured {
		*cfg, err = getPlatformStorage()
		if err != nil {
			return nil, fmt.Errorf("unable to get storage configuration from cluster install config: %s", err)
		}
		return newDriver(cfg)
	}
	return drv, nil
}

// getPlatformStorage returns the storage configuration that should be used
// based on the cloudplatform we are running on, as determined from the
// installer configuration.
func getPlatformStorage() (imageregistryv1.ImageRegistryConfigStorage, error) {
	var cfg imageregistryv1.ImageRegistryConfigStorage

	installConfig, err := clusterconfig.GetInstallConfig()
	if err != nil {
		return cfg, err
	}

	switch {
	case installConfig.Platform.Libvirt != nil:
		cfg.Filesystem = &imageregistryv1.ImageRegistryConfigStorageFilesystem{
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}
	case installConfig.Platform.AWS != nil:
		cfg.S3 = &imageregistryv1.ImageRegistryConfigStorageS3{}
	default:
		// if we can't determine what platform we're on, fallback to creating
		// a PVC for the registry.
		// TODO: implement this idea
		return cfg, nil
	}

	return cfg, nil
}
