package storage

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/azure"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/emptydir"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/gcs"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/pvc"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/swift"
)

var (
	ErrStorageNotConfigured = fmt.Errorf("storage backend not configured")
)

// MultiStoragesError is returned when we have multiple storage engines
// configured and we can't determin which one the user wants to use.
type MultiStoragesError struct {
	names []string
}

// Error return MultiStoragesError as string.
func (m *MultiStoragesError) Error() string {
	if m == nil {
		return "<nil>"
	}
	return fmt.Sprintf(
		"exactly one storage type should be configured at the same time, got %d: %v",
		len(m.names), m.names,
	)
}

type Driver interface {
	ConfigEnv() ([]corev1.EnvVar, error)
	Volumes() ([]corev1.Volume, []corev1.VolumeMount, error)
	Secrets() (map[string]string, error)
	CreateStorage(*imageregistryv1.Config) error
	StorageExists(*imageregistryv1.Config) (bool, error)
	RemoveStorage(*imageregistryv1.Config) (bool, error)
	StorageChanged(*imageregistryv1.Config) bool
	ID() string
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
		ctx := context.Background()
		drivers = append(drivers, s3.NewDriver(ctx, cfg.S3, kubeconfig, listers))
	}

	if cfg.Swift != nil {
		names = append(names, "Swift")
		drivers = append(drivers, swift.NewDriver(cfg.Swift, listers))
	}

	if cfg.GCS != nil {
		names = append(names, "GCS")
		ctx := context.Background()
		drivers = append(drivers, gcs.NewDriver(ctx, cfg.GCS, kubeconfig, listers))
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
		ctx := context.Background()
		drivers = append(drivers, azure.NewDriver(ctx, cfg.Azure, kubeconfig, listers))
	}

	switch len(drivers) {
	case 0:
		return nil, ErrStorageNotConfigured
	case 1:
		return drivers[0], nil
	}

	return nil, &MultiStoragesError{names}
}

func NewDriver(cfg *imageregistryv1.ImageRegistryConfigStorage, kubeconfig *rest.Config, listers *regopclient.Listers) (Driver, error) {
	drv, err := newDriver(cfg, kubeconfig, listers)
	if err == ErrStorageNotConfigured {
		*cfg, err = GetPlatformStorage(listers)
		if err != nil {
			return nil, fmt.Errorf("unable to get storage configuration from cluster install config: %s", err)
		}
		drv, err = newDriver(cfg, kubeconfig, listers)
	}
	return drv, err
}

// GetPlatformStorage returns the storage configuration that should be used
// based on the cloudplatform we are running on, as determined from the
// infrastructure configuration.
//
// Following rules apply:
// - If it is a known platform for which we have a backend implementation (e.g.
//   AWS) we return a storage configuration that uses that implementation.
// - If it is a known platform and it doesn't provide any backend implementation,
//   we return an empty storage configuration.
// - If it is a unknown platform we return a storage configuration with EmptyDir.
//   This is useful as it easily allows other teams to experiment with OpenShift
//   in new platforms, if it is LibVirt platform we also return EmptyDir for
//   historical reasons.
func GetPlatformStorage(listers *regopclient.Listers) (imageregistryv1.ImageRegistryConfigStorage, error) {
	var cfg imageregistryv1.ImageRegistryConfigStorage

	infra, err := listers.Infrastructures.Get("cluster")
	if err != nil {
		return imageregistryv1.ImageRegistryConfigStorage{}, err
	}

	switch infra.Status.PlatformStatus.Type {

	// These are the platforms we don't configure any backend for, on these
	// we should bootstrap the image registry as "Removed".
	case configapiv1.BareMetalPlatformType,
		configapiv1.OvirtPlatformType,
		configapiv1.VSpherePlatformType,
		configapiv1.NonePlatformType:
		break

	// These are the supported platforms. We do have backend implementation
	// for them.
	case configapiv1.AWSPlatformType:
		cfg.S3 = &imageregistryv1.ImageRegistryConfigStorageS3{}
	case configapiv1.AzurePlatformType:
		cfg.Azure = &imageregistryv1.ImageRegistryConfigStorageAzure{}
	case configapiv1.GCPPlatformType:
		cfg.GCS = &imageregistryv1.ImageRegistryConfigStorageGCS{}
	case configapiv1.OpenStackPlatformType:
		cfg.Swift = &imageregistryv1.ImageRegistryConfigStorageSwift{}

	// Unknown platforms or LibVirt: we configure image registry using
	// EmptyDir storage.
	case configapiv1.LibvirtPlatformType:
		fallthrough
	default:
		cfg.EmptyDir = &imageregistryv1.ImageRegistryConfigStorageEmptyDir{}
	}

	return cfg, nil
}
