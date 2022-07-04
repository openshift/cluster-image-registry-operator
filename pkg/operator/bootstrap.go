package operator

import (
	"context"
	"fmt"

	appsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/pvc"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

// Bootstrap registers this operator with OpenShift by creating an appropriate
// ClusterOperator custom resource. This function also creates the initial
// configuration for the Image Registry.
func (c *Controller) Bootstrap() error {
	cr, err := c.listers.RegistryConfigs.Get(defaults.ImageRegistryResourceName)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("unable to get the registry custom resources: %s", err)
	}

	// If the registry resource already exists, no bootstrapping is required
	if cr != nil {
		return nil
	}

	// If no registry resource exists, let's create one with sane defaults
	klog.Infof("generating registry custom resource")

	platformStorage, replicas, err := storage.GetPlatformStorage(&c.listers.StorageListers)
	if err != nil {
		return err
	}

	infra, err := util.GetInfrastructure(&c.listers.StorageListers)
	if err != nil {
		return fmt.Errorf("unable to get infrastructure resource: %w", err)
	}

	if infra.Status.InfrastructureTopology == configapiv1.SingleReplicaTopologyMode && replicas > 1 {
		replicas = 1
	}

	noStorage := imageregistryv1.ImageRegistryConfigStorage{}

	// We bootstrap as "Removed" if the platform is known and does not
	// provide persistent storage out of the box. If the platform is
	// unknown we will bootstrap as Managed but using EmptyDir storage
	// engine(ephemeral).
	mgmtState := operatorapi.Managed
	if platformStorage == noStorage {
		mgmtState = operatorapi.Removed
	}

	rolloutStrategy := appsapi.RollingUpdateDeploymentStrategyType
	if platformStorage.PVC != nil {
		if err = c.createPVC(corev1.ReadWriteOnce, platformStorage.PVC.Claim); err != nil {
			return err
		}
		rolloutStrategy = appsapi.RecreateDeploymentStrategyType
	}

	cr = &imageregistryv1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:       defaults.ImageRegistryResourceName,
			Finalizers: []string{defaults.ImageRegistryOperatorResourceFinalizer},
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			OperatorSpec: operatorapi.OperatorSpec{
				LogLevel:         operatorapi.Normal,
				OperatorLogLevel: operatorapi.Normal,
			},
			ManagementState: mgmtState,
			Storage:         platformStorage,
			Replicas:        replicas,
			RolloutStrategy: string(rolloutStrategy),
		},
		Status: imageregistryv1.ImageRegistryStatus{},
	}

	if _, err = c.clients.RegOp.ImageregistryV1().Configs().Create(
		context.TODO(), cr, metav1.CreateOptions{},
	); err != nil {
		return err
	}

	return nil
}

func (c *Controller) createPVC(accessMode corev1.PersistentVolumeAccessMode, claimName string) error {
	// Check that the claim does not exist before creating it
	if _, err := c.clients.Core.PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).Get(
		context.TODO(), claimName, metav1.GetOptions{},
	); err == nil {
		return nil
	} else if !errors.IsNotFound(err) {
		return err
	}

	// "standard-csi" is the default StorageClass name in 4.11 and newer versions, that was provisioned by the cloud provider
	storageClassName := "standard-csi"

	// This is a Workaround for Bug#1862991 Tracker for removel on Bug#1866240
	if infra, err := util.GetInfrastructure(&c.listers.StorageListers); err != nil {
		return err
	} else if infra.Status.PlatformStatus.Type == configapiv1.OvirtPlatformType {
		storageClassName = "ovirt-csi-sc"
	}

	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: defaults.ImageRegistryOperatorNamespace,
			Annotations: map[string]string{
				pvc.PVCOwnerAnnotation: "true",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				accessMode,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("100Gi"),
				},
			},
			StorageClassName: &storageClassName,
		},
	}

	_, err := c.clients.Core.PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).Create(
		context.TODO(), claim, metav1.CreateOptions{},
	)
	return err
}
