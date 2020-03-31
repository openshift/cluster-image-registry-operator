package pvc

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

const (
	rootDirectory      = "/registry"
	PVCOwnerAnnotation = "imageregistry.openshift.io"
)

type driver struct {
	Namespace string
	Config    *imageregistryv1.ImageRegistryConfigStoragePVC
	Client    *coreset.CoreV1Client
}

func NewDriver(c *imageregistryv1.ImageRegistryConfigStoragePVC, kubeconfig *rest.Config) (*driver, error) {
	namespace, err := regopclient.GetWatchNamespace()
	if err != nil {
		return nil, fmt.Errorf("failed to get watch namespace: %s", err)
	}

	client, err := coreset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	return &driver{
		Namespace: namespace,
		Config:    c,
		Client:    client,
	}, nil
}

func (d *driver) ConfigEnv() (envs envvar.List, err error) {
	envs = append(envs,
		envvar.EnvVar{Name: "REGISTRY_STORAGE", Value: "filesystem"},
		envvar.EnvVar{Name: "REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY", Value: rootDirectory},
	)
	return
}

func (d *driver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	vol := corev1.Volume{
		Name: "registry-storage",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: d.Config.Claim,
			},
		},
	}

	mount := corev1.VolumeMount{
		Name:      vol.Name,
		MountPath: rootDirectory,
	}

	return []corev1.Volume{vol}, []corev1.VolumeMount{mount}, nil
}

func (d *driver) VolumeSecrets() (map[string]string, error) {
	return nil, nil
}

func (d *driver) Secrets() (map[string]string, error) {
	return nil, nil
}

func (d *driver) StorageExists(cr *imageregistryv1.Config) (bool, error) {
	if len(d.Config.Claim) != 0 {
		_, err := d.Client.PersistentVolumeClaims(d.Namespace).Get(
			context.TODO(), d.Config.Claim, metav1.GetOptions{},
		)
		if err == nil {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "PVC Exists", "")
			return true, nil
		}
		if !errors.IsNotFound(err) {
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, fmt.Sprintf("Unknown error occurred checking for volume claim %s", d.Config.Claim), err.Error())
			return false, err
		}
	}
	util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "PVC does not exist", "")
	return false, nil
}

func (d *driver) StorageChanged(cr *imageregistryv1.Config) bool {
	if !reflect.DeepEqual(cr.Status.Storage.PVC, cr.Spec.Storage.PVC) {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionUnknown, "PVC Configuration Changed", "PVC storage is in an old state")
		return true
	}
	return false
}

func (d *driver) checkPVC(cr *imageregistryv1.Config, claim *corev1.PersistentVolumeClaim) (err error) {
	if claim == nil {
		claim, err = d.Client.PersistentVolumeClaims(d.Namespace).Get(
			context.TODO(), d.Config.Claim, metav1.GetOptions{},
		)
		if err != nil {
			return err
		}
	}

	// Check what access modes are available.

	// We allow using RWO PV backend, but it has some limitations:
	// 1. Image registry rollout strategy must be set to Recreate (default is RollingUpdate).
	// 2. It's not possible to use more than 1 replica of the image registry.

	// RWX backends are accepted with no additional conditions.
	rwoModeEnabled := false
	for _, claimMode := range claim.Spec.AccessModes {
		if claimMode == corev1.ReadWriteMany {
			return nil
		}
		if claimMode == corev1.ReadWriteOnce {
			rwoModeEnabled = true
		}
	}

	if rwoModeEnabled {
		if cr.Spec.Replicas > 1 {
			return fmt.Errorf("cannot use %s access mode with more than one replica of the image registry", corev1.ReadWriteOnce)
		}

		if cr.Spec.RolloutStrategy != string(appsv1.RecreateDeploymentStrategyType) {
			return fmt.Errorf("cannot use %s access mode with %s rollout strategy", corev1.ReadWriteOnce, cr.Spec.RolloutStrategy)
		}

		return nil
	}

	return fmt.Errorf("PVC %s does not contain the necessary access modes: %s or %s", d.Config.Claim, corev1.ReadWriteMany, corev1.ReadWriteOnce)
}

func (d *driver) createPVC(cr *imageregistryv1.Config) (*corev1.PersistentVolumeClaim, error) {
	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.Config.Claim,
			Namespace: d.Namespace,
			Annotations: map[string]string{
				PVCOwnerAnnotation: "true",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteMany,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("100Gi"),
				},
			},
		},
	}

	return d.Client.PersistentVolumeClaims(d.Namespace).Create(
		context.TODO(), claim, metav1.CreateOptions{},
	)
}

func (d *driver) CreateStorage(cr *imageregistryv1.Config) error {
	var (
		err            error
		claim          *corev1.PersistentVolumeClaim
		storageManaged bool
	)

	if len(d.Config.Claim) == 0 {
		d.Config.Claim = defaults.PVCImageRegistryName

		// If there is no name and there is no PVC, then we will create a PVC.
		// If PVC is there and it was created by us, then just start using it again.
		storageManaged = true

		claim, err = d.Client.PersistentVolumeClaims(d.Namespace).Get(
			context.TODO(), d.Config.Claim, metav1.GetOptions{},
		)
		if err == nil {
			if !pvcIsCreatedByOperator(claim) {
				err = fmt.Errorf("could not create default PVC, it already exists and is not owned by the operator")
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "PVC Already Exists", err.Error())
				return err
			}
		} else if errors.IsNotFound(err) {
			claim, err = d.createPVC(cr)
			if err != nil {
				util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "Creation Failed", err.Error())
				return err
			}
			util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "PVC Created", "")
		} else {
			return err
		}
	} else {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionTrue, "PVC Exists", "")
	}

	if err := d.checkPVC(cr, claim); err != nil {
		util.UpdateCondition(cr, defaults.StorageExists, operatorapi.ConditionFalse, "PVC Issues Found", err.Error())
		return err
	}

	cr.Status.StorageManaged = storageManaged
	cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{
		PVC: d.Config.DeepCopy(),
	}
	cr.Spec.Storage.PVC = d.Config.DeepCopy()

	return nil
}

func (d *driver) RemoveStorage(cr *imageregistryv1.Config) (retriable bool, err error) {
	if !cr.Status.StorageManaged || len(d.Config.Claim) == 0 {
		return false, nil
	}

	err = d.Client.PersistentVolumeClaims(d.Namespace).Delete(
		context.TODO(), d.Config.Claim, metav1.DeleteOptions{},
	)
	if err != nil && !errors.IsNotFound(err) {
		return false, err
	}

	return false, nil
}

func pvcIsCreatedByOperator(claim *corev1.PersistentVolumeClaim) (exist bool) {
	_, exist = claim.Annotations[PVCOwnerAnnotation]
	return
}

// ID return the underlying storage identificator, on this case the claim name.
func (d *driver) ID() string {
	return d.Config.Claim
}
