package emptydir

import (
	"reflect"

	operatorapi "github.com/openshift/api/operator/v1"

	corev1 "k8s.io/api/core/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

const (
	rootDirectory = "/registry"
)

type driver struct {
	Config  *imageregistryv1.ImageRegistryConfigStorageEmptyDir
	Listers *regopclient.Listers
}

func NewDriver(c *imageregistryv1.ImageRegistryConfigStorageEmptyDir, listers *regopclient.Listers) *driver {
	return &driver{
		Config:  c,
		Listers: listers,
	}
}

func (d *driver) Secrets() (map[string]string, error) {
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
		Name: "registry-storage",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	mount := corev1.VolumeMount{
		Name:      vol.Name,
		MountPath: rootDirectory,
	}

	return []corev1.Volume{vol}, []corev1.VolumeMount{mount}, nil
}

func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	return true, nil
}

func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.EmptyDir, cr.Spec.Storage.EmptyDir) {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "EmptyDir Configuration Changed", "EmptyDir storage is in an unknown state")
		return true
	}

	return false
}

func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	if !reflect.DeepEqual(cr.Status.Storage.EmptyDir, cr.Spec.Storage.EmptyDir) {
		cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
			EmptyDir: d.Config.DeepCopy(),
		}
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "Creation Successful", "EmptyDir storage successfully created")
	}

	return nil
}

func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (bool, error) {
	return false, nil
}

// ID return the underlying storage identificator, on this case as we don't
// have any id we always return an empty string.
func (d *driver) ID() string {
	return ""
}
