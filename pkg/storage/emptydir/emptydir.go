package emptydir

import (
	"reflect"

	operatorapi "github.com/openshift/api/operator/v1"

	coreapi "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

const (
	rootDirectory = "/registry"
)

type driver struct {
	Config    *opapi.ImageRegistryConfigStorageFilesystem
}

func NewDriver(c *opapi.ImageRegistryConfigStorageFilesystem) *driver {
	return &driver{
		Config:    c,
	}
}

func (d *driver) UpdateFromStorage(cfg opapi.ImageRegistryConfigStorage) {
	d.Config = cfg.Filesystem.DeepCopy()
}

func (d *driver) GetType() string {
	return string(clusterconfig.StorageTypeEmptyDir)
}

func (d *driver) SyncSecrets(sec *coreapi.Secret) (map[string]string, error) {
	return nil, nil
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: "filesystem"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY", Value: rootDirectory},
	)

	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	vol := corev1.Volume{
		Name:         "registry-storage",
		VolumeSource: d.Config.VolumeSource,
	}

	mount := corev1.VolumeMount{
		Name:      vol.Name,
		MountPath: rootDirectory,
	}

	return []corev1.Volume{vol}, []corev1.VolumeMount{mount}, nil
}

func (d *driver) StorageExists(cr *opapi.ImageRegistry, modified *bool) (bool, error) {
	return true, nil
}

func (d *driver) StorageChanged(cr *opapi.ImageRegistry, modified *bool) bool {
	if !reflect.DeepEqual(cr.Status.Storage.Filesystem, cr.Spec.Storage.Filesystem) {
		util.UpdateCondition(cr, opapi.StorageExists, operatorapi.ConditionUnknown, "EmptyDir Configuration Changed", "EmptyDir storage is in an unknown state", modified)
		return true
	}

	return false
}

func (d *driver) GetStorageName() string {
	return "EmptyDir"
}

func (d *driver) CreateStorage(cr *opapi.ImageRegistry, modified *bool) error {
	if !reflect.DeepEqual(cr.Status.Storage.Filesystem, cr.Spec.Storage.Filesystem) {
		cr.Status.Storage.Filesystem = d.Config.DeepCopy()
		*modified = true
	}

	return nil
}

func (d *driver) RemoveStorage(cr *opapi.ImageRegistry, modified *bool) error {
	return nil
}

func (d *driver) CompleteConfiguration(cr *opapi.ImageRegistry, modified *bool) error {

	cr.Spec.Storage.Filesystem = d.Config.DeepCopy()
	*modified = true
	return nil
}
