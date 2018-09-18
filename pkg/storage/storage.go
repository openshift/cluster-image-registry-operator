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

func NewDriver(cfg *opapi.OpenShiftDockerRegistryConfigStorage) (drv Driver, err error) {
	storageConfigured := 0

	if cfg.Azure != nil {
		drv = azure.NewDriver(cfg.Azure)
		storageConfigured += 1
	}

	if cfg.Filesystem != nil {
		drv = filesystem.NewDriver(cfg.Filesystem)
		storageConfigured += 1
	}

	if cfg.GCS != nil {
		drv = gcs.NewDriver(cfg.GCS)
		storageConfigured += 1
	}

	if cfg.S3 != nil {
		drv = s3.NewDriver(cfg.S3)
		storageConfigured += 1
	}

	if cfg.Swift != nil {
		drv = swift.NewDriver(cfg.Swift)
		storageConfigured += 1
	}

	if storageConfigured != 1 {
		err = fmt.Errorf("it is not possible to initialize more than one storage backend at the same time")
		drv = nil
	}

	return
}
