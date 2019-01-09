package storage

import (
	"fmt"

	"github.com/golang/glog"

	coreapi "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
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
	GetType() string
	ConfigEnv() ([]corev1.EnvVar, error)
	Volumes() ([]corev1.Volume, []corev1.VolumeMount, error)
	CompleteConfiguration(*opapi.ImageRegistry, *bool) error
	CreateStorage(*opapi.ImageRegistry, *bool) error
	StorageExists(*opapi.ImageRegistry, *bool) (bool, error)
	RemoveStorage(*opapi.ImageRegistry, *bool) error
	StorageChanged(*opapi.ImageRegistry, *bool) bool
	GetStorageName() string
	SyncSecrets(*coreapi.Secret) (map[string]string, error)
	UpdateFromStorage(cfg opapi.ImageRegistryConfigStorage)
}

func newDriver(cfg *opapi.ImageRegistryConfigStorage) (Driver, error) {
	var drivers []Driver

	if cfg.Azure != nil {
		drivers = append(drivers, azure.NewDriver(cfg.Azure))
	}

	if cfg.Filesystem != nil && cfg.Filesystem.VolumeSource.EmptyDir == nil {
		drivers = append(drivers, filesystem.NewDriver(cfg.Filesystem))
	}

	if cfg.Filesystem != nil && cfg.Filesystem.VolumeSource.EmptyDir != nil {
		drivers = append(drivers, emptydir.NewDriver(cfg.Filesystem))
	}

	if cfg.GCS != nil {
		drivers = append(drivers, gcs.NewDriver(cfg.GCS))
	}

	if cfg.S3 != nil {
		drivers = append(drivers, s3.NewDriver(cfg.S3))
	}

	if cfg.Swift != nil {
		drivers = append(drivers, swift.NewDriver(cfg.Swift))
	}

	switch len(drivers) {
	case 0:
		return nil, ErrStorageNotConfigured
	case 1:
		return drivers[0], nil
	}

	var names []string
	for _, drv := range drivers {
		names = append(names, drv.GetType())
	}

	return nil, fmt.Errorf("exactly one storage type should be configured at the same time, got %d: %v", len(drivers), names)
}

func NewDriver(cfg *opapi.ImageRegistryConfigStorage) (Driver, error) {
	drv, err := newDriver(cfg)
	if err == ErrStorageNotConfigured {
		storageType, err := getPlatformStorage()
		if err != nil {
			return nil, fmt.Errorf("unable to get storage type from cluster install config: %s", err)
		}
		switch storageType {
		case clusterconfig.StorageTypeAzure:
			cfg.Azure = &opapi.ImageRegistryConfigStorageAzure{}
		case clusterconfig.StorageTypeFileSystem:
			cfg.Filesystem = &opapi.ImageRegistryConfigStorageFilesystem{}
		case clusterconfig.StorageTypeEmptyDir:
			cfg.Filesystem = &opapi.ImageRegistryConfigStorageFilesystem{
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			}
		case clusterconfig.StorageTypeGCS:
			cfg.GCS = &opapi.ImageRegistryConfigStorageGCS{}
		case clusterconfig.StorageTypeS3:
			cfg.S3 = &opapi.ImageRegistryConfigStorageS3{}
		case clusterconfig.StorageTypeSwift:
			cfg.Swift = &opapi.ImageRegistryConfigStorageSwift{}
		default:
			glog.Errorf("unknown storage backend: %s", storageType)
			return nil, ErrStorageNotConfigured
		}
		return newDriver(cfg)
	}
	return drv, nil
}

// getPlatformStorage returns the storage type that should be used
// based on the cloudplatform we are running on, as determined
// from the installer configuration.
func getPlatformStorage() (clusterconfig.StorageType, error) {
	installConfig, err := clusterconfig.GetInstallConfig()
	if err != nil {
		return "", err
	}

	// if we can't determine what platform we're on, fallback to creating
	// a PVC for the registry.
	switch {
	case installConfig.Platform.Libvirt != nil:
		return clusterconfig.StorageTypeEmptyDir, nil
	case installConfig.Platform.AWS != nil:
		return clusterconfig.StorageTypeS3, nil
		// case installConfig.Platform.OpenStack != nil:
		// 	return clusterconfig.StorageTypeSwift, nil
	default:
		//return clusterconfig.StorageTypeFileSystem, nil
		return "", nil
	}
}
