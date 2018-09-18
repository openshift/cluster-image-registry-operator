package storage

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"

	"github.com/openshift/cluster-image-registry-operator/pkg/storage/azure"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/filesystem"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/gcs"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/swift"
)

type Driver interface {
	GetName() string
	ConfigEnv() ([]corev1.EnvVar, error)
	Volumes() ([]corev1.Volume, []corev1.VolumeMount, error)
}

func NewDriver(cfg *opapi.OpenShiftDockerRegistryConfigStorage) (Driver, error) {
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

	if len(drivers) != 1 {
		var names []string
		for _, drv := range drivers {
			names = append(names, drv.GetName())
		}
		return nil, fmt.Errorf("exactly one storage backend should be configured at the same time, got %d: %v", len(drivers), names)
	}

	return drivers[0], nil
}
