package storage

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	installer "github.com/openshift/installer/pkg/types"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
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
	GetName() string
	ConfigEnv() ([]corev1.EnvVar, error)
	Volumes() ([]corev1.Volume, []corev1.VolumeMount, error)
	CompleteConfiguration() error
	ValidateConfiguration(*opapi.ImageRegistry, *bool) error
}

func newDriver(crname string, crnamespace string, cfg *opapi.ImageRegistryConfigStorage) (Driver, error) {
	var drivers []Driver

	if cfg.Azure != nil {
		drivers = append(drivers, azure.NewDriver(crname, crnamespace, cfg.Azure))
	}

	if cfg.Filesystem != nil && cfg.Filesystem.VolumeSource.EmptyDir == nil {
		drivers = append(drivers, filesystem.NewDriver(crname, crnamespace, cfg.Filesystem))
	}

	if cfg.Filesystem != nil && cfg.Filesystem.VolumeSource.EmptyDir != nil {
		drivers = append(drivers, emptydir.NewDriver(crname, crnamespace, nil))
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
		storageType, err := getPlatformStorage()
		if err != nil {
			return nil, fmt.Errorf("unable to get storage type from cluster install config: %s", err)
		}
		switch strings.ToLower(storageType) {
		case "azure":
			cfg.Azure = &opapi.ImageRegistryConfigStorageAzure{}
		case "filesystem":
			cfg.Filesystem = &opapi.ImageRegistryConfigStorageFilesystem{}
		case "emptydir":
			cfg.Filesystem = &opapi.ImageRegistryConfigStorageFilesystem{
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			}
		case "gcs":
			cfg.GCS = &opapi.ImageRegistryConfigStorageGCS{}
		case "s3":
			cfg.S3 = &opapi.ImageRegistryConfigStorageS3{}
		case "swift":
			cfg.Swift = &opapi.ImageRegistryConfigStorageSwift{}
		default:
			logrus.Errorf("unknown storage backend: %s", storageType)
			return nil, ErrStorageNotConfigured
		}
		return newDriver(crname, crnamespace, cfg)
	}
	return drv, nil
}

// getPlatformStorage returns the storage type that should be used
// based on the cloudplatform we are running on, as determined
// from the installer configuration.
func getPlatformStorage() (string, error) {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-config-v1",
			Namespace: "kube-system",
		},
	}

	if err := sdk.Get(cm); err != nil {
		return "", fmt.Errorf("unable to read cluster install configuration: %v", err)
	}

	installConfig := &installer.InstallConfig{}
	if err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(cm.Data["install-config"]), 100).Decode(installConfig); err != nil {
		return "", fmt.Errorf("unable to decode cluster install configuration: %v", err)
	}

	// if we can't determine what platform we're on, fallback to creating
	// a PVC for the registry.
	storage := ""
	switch {
	case installConfig.Platform.Libvirt != nil:
		storage = "emptydir"
		// TODO - Corey enable s3, someone enable swift in the future.
		/*
			case installConfig.Platform.AWS != nil:
				storage = "s3"
			case installConfig.Platform.OpenStack != nil:
				storage = "swift"
			default:
			  	storage="filesystem"
		*/
	}
	return storage, nil
}
