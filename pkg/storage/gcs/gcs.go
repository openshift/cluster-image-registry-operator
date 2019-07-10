package gcs

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"

	operatorapi "github.com/openshift/api/operator/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

type GCS struct {
	Bucket      string
	KeyfileData string
}

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

// GetConfig reads configuration for the GCS cloud platform services.
func GetConfig(listers *regopclient.Listers) (*GCS, error) {
	cfg := &GCS{}

	// Look for a user defined secret to get the GCS credentials from
	sec, err := listers.Secrets.Get(imageregistryv1.ImageRegistryPrivateConfigurationUser)
	if err != nil {
		return nil, err
	} else {
		// GCS credentials are stored in a file that can be downloaded from the
		// GCP console
		if v, ok := sec.Data["REGISTRY_STORAGE_GCS_KEYFILE"]; ok {
			cfg.KeyfileData = string(v)
		} else {
			return nil, fmt.Errorf("secret %q does not contain required key \"REGISTRY_STORAGE_GCS_KEYFILE\"", fmt.Sprintf("%s/%s", imageregistryv1.ImageRegistryOperatorNamespace, imageregistryv1.ImageRegistryPrivateConfigurationUser))
		}
	}

	return cfg, nil
}

func (d *driver) Secrets() (map[string]string, error) {
	cfg, err := GetConfig(d.Listers)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"REGISTRY_STORAGE_GCS_KEYFILE": cfg.KeyfileData,
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
						Key:  "REGISTRY_STORAGE_GCS_KEYFILE",
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
