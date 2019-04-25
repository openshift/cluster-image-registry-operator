package gcs

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"

	operatorapi "github.com/openshift/api/operator/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

type driver struct {
	Config  *imageregistryv1.ImageRegistryConfigStorageGCS
	Listers *regopclient.Listers
}

func NewDriver(c *imageregistryv1.ImageRegistryConfigStorageGCS, listers *regopclient.Listers) *driver {
	return &driver{
		Config:  c,
		Listers: listers,
	}
}

func (d *driver) Secrets() (map[string]string, error) {
	cfg, err := clusterconfig.GetGCSConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"STORAGE_GCS_KEYFILE": cfg.Storage.GCS.KeyfileData,
	}, nil
}

func (d *driver) ConfigEnv() (envs []corev1.EnvVar, err error) {
	envs = append(envs,
		corev1.EnvVar{Name: "REGISTRY_STORAGE", Value: "gcs"},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_GCS_BUCKET", Value: d.Config.Bucket},
		corev1.EnvVar{Name: "REGISTRY_STORAGE_GCS_KEYFILE", Value: "/gcs/keyfile"},
	)
	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	optional := false

	vol := corev1.Volume{
		Name: "registry-storage-keyfile",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: imageregistryv1.ImageRegistryPrivateConfiguration,
				Items: []corev1.KeyToPath{
					{
						Key:  "STORAGE_GCS_KEYFILE",
						Path: "keyfile",
					},
				},
				Optional: &optional,
			},
		},
	}

	mount := corev1.VolumeMount{
		Name:      vol.Name,
		MountPath: "/gcs",
	}

	return []corev1.Volume{vol}, []corev1.VolumeMount{mount}, nil
}

func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	return false, nil
}

func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.GCS, cr.Spec.Storage.GCS) {
		util.UpdateCondition(cr, imageregistryv1.StorageExists, operatorapi.ConditionUnknown, "GCS Configuration Changed", "GCS storage is in an unknown state")
		return true
	}

	return false
}

func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	if !reflect.DeepEqual(cr.Status.Storage.GCS, d.Config) {
		cr.Status.Storage.GCS = d.Config.DeepCopy()
	}
	return nil
}

func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (bool, error) {
	if !cr.Status.StorageManaged {
		return false, nil
	}

	return false, nil
}

func (d *driver) CompleteConfiguration(cr *imageregistryv1.Config) error {
	return nil
}
