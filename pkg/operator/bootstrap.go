package operator

import (
	"context"
	"crypto/rand"
	"fmt"

	appsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/pvc"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/swift"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

// randomSecretSize is the number of random bytes to generate
// for the http secret
const randomSecretSize = 64

// Bootstrap registers this operator with OpenShift by creating an appropriate
// ClusterOperator custom resource. This function also creates the initial
// configuration for the Image Registry.
func (c *Controller) Bootstrap() error {
	cr, err := c.listers.RegistryConfigs.Get(defaults.ImageRegistryResourceName)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("unable to get the registry custom resources: %s", err)
	}

	// If the registry resource already exists,
	// no bootstrapping is required
	if cr != nil {
		return nil
	}

	// If no registry resource exists,
	// let's create one with sane defaults
	klog.Infof("generating registry custom resource")

	var secretBytes [randomSecretSize]byte
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
	}

	platformStorage, replicas, err := storage.GetPlatformStorage(c.listers)
	if err != nil {
		return err
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

	infra, err := util.GetInfrastructure(c.listers)
	if err != nil {
		return err
	}

	rolloutStrategy := appsapi.RollingUpdateDeploymentStrategyType

	// If Swift service is not available for OpenStack, we have to start using
	// Cinder with RWO PVC backend. It means that we need to create an RWO claim
	// and set the rollout strategy to Recreate.
	switch infra.Status.PlatformStatus.Type {
	case configapiv1.OpenStackPlatformType:
		isSwiftEnabled, err := swift.IsSwiftEnabled(c.listers)
		if err != nil {
			return err
		}
		if !isSwiftEnabled {
			err = c.createPVC(corev1.ReadWriteOnce)
			if err != nil {
				return err
			}

			rolloutStrategy = appsapi.RecreateDeploymentStrategyType
		}
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
			HTTPSecret:      fmt.Sprintf("%x", string(secretBytes[:])),
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

func (c *Controller) createPVC(accessMode corev1.PersistentVolumeAccessMode) error {
	claimName := defaults.PVCImageRegistryName

	// Check that the claim does not exist before creating it
	_, err := c.clients.Core.PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).Get(
		context.TODO(), claimName, metav1.GetOptions{},
	)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// "standard" is the default StorageClass name, that was provisioned by the cloud provider
		storageClassName := "standard"

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

		_, err = c.clients.Core.PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).Create(
			context.TODO(), claim, metav1.CreateOptions{},
		)
		if err != nil {
			return err
		}
	}

	return nil
}
