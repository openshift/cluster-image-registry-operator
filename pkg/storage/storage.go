package storage

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/azure"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/filesystem"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/gcs"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/swift"
)

var (
	ErrStorageNotConfigured = fmt.Errorf("storage backend not configured")
)

type Driver interface {
	GetName() string
	ConfigEnv() ([]corev1.EnvVar, error)
	Volumes() ([]corev1.Volume, []corev1.VolumeMount, error)
	CompleteConfiguration() error
	ValidateConfiguration(*corev1.ConfigMap) error
}

func newDriver(crname string, crnamespace string, cfg *opapi.ImageRegistryConfigStorage) (Driver, error) {
	var drivers []Driver

	if cfg.Azure != nil {
		drivers = append(drivers, azure.NewDriver(crname, crnamespace, cfg.Azure))
	}

	if cfg.Filesystem != nil {
		drivers = append(drivers, filesystem.NewDriver(crname, crnamespace, cfg.Filesystem))
	}

	if cfg.GCS != nil {
		drivers = append(drivers, gcs.NewDriver(crname, crnamespace, cfg.GCS))
	}

	if cfg.S3 != nil {
		drivers = append(drivers, s3.NewDriver(crname, crnamespace, cfg.S3))
	}

	if cfg.Swift != nil {
		drivers = append(drivers, swift.NewDriver(crname, crnamespace, cfg.Swift))
	}

	switch len(drivers) {
	case 0:
		return nil, ErrStorageNotConfigured
	case 1:
		return drivers[0], nil
	}

	var names []string
	for _, drv := range drivers {
		names = append(names, drv.GetName())
	}

	return nil, fmt.Errorf("exactly one storage backend should be configured at the same time, got %d: %v", len(drivers), names)
}

func NewDriver(crname string, crnamespace string, cfg *opapi.ImageRegistryConfigStorage) (Driver, error) {
	drv, err := newDriver(crname, crnamespace, cfg)
	if err == ErrStorageNotConfigured {
		gcfg, err := clusterconfig.Get()
		if err != nil {
			logrus.Errorf("unable to get global config: %s", err)
			return nil, ErrStorageNotConfigured
		}
		switch strings.ToLower(gcfg.Storage.Type) {
		case "azure":
			cfg.Azure = &opapi.ImageRegistryConfigStorageAzure{}
		case "filesystem":
			cfg.Filesystem = &opapi.ImageRegistryConfigStorageFilesystem{}
		case "gcs":
			cfg.GCS = &opapi.ImageRegistryConfigStorageGCS{}
		case "s3":
			cfg.S3 = &opapi.ImageRegistryConfigStorageS3{}
		case "swift":
			cfg.Swift = &opapi.ImageRegistryConfigStorageSwift{}
		default:
			return nil, fmt.Errorf("unknown storage backend: %s", gcfg.Storage.Type)
		}
		return newDriver(crname, crnamespace, cfg)
	}
	return drv, nil
}
