package storage

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"

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

func NewDriver(cfg *opapi.ImageRegistryConfigStorage) (Driver, error) {
	var drivers []Driver

	if cfg.Azure != nil {
		drivers = append(drivers, azure.NewDriver(cfg.Azure))
	}

	if cfg.Filesystem != nil {
		drivers = append(drivers, filesystem.NewDriver(cfg.Filesystem))
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
		names = append(names, drv.GetName())
	}

	return nil, fmt.Errorf("exactly one storage backend should be configured at the same time, got %d: %v", len(drivers), names)
}
