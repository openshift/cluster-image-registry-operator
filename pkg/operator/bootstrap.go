package operator

import (
	"crypto/rand"
	"fmt"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsapi "github.com/openshift/api/apps/v1"
	operatorapi "github.com/openshift/api/operator/v1alpha1"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/migration"
	"github.com/openshift/cluster-image-registry-operator/pkg/migration/dependency"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

// randomSecretSize is the number of random bytes to generate if no secret
// was specified.
const randomSecretSize = 64

func resourceName(namespace string) string {
	if namespace == "default" {
		return "docker-registry"
	}
	return "image-registry"
}

func (h *Handler) Bootstrap() (*imageregistryapi.ImageRegistry, error) {
	// TODO(legion): Add real bootstrap based on global ConfigMap or something.

	crList := &imageregistryapi.ImageRegistryList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapi.SchemeGroupVersion.String(),
			Kind:       "ImageRegistry",
		},
	}

	err := sdk.List(h.params.Deployment.Namespace, crList)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to list registry custom resources: %s", err)
		}
	}

	switch len(crList.Items) {
	case 0:
		// let's create it.
	case 1:
		if crList.Items[0].ObjectMeta.DeletionTimestamp != nil {
			err = h.finalizeResources(&crList.Items[0])
			if err != nil {
				return nil, err
			}
		} else {
			return &crList.Items[0], nil
		}
	default:
		return nil, fmt.Errorf("only one registry custom resource expected in %s namespace, got %d", h.params.Deployment.Namespace, len(crList.Items))
	}

	var spec imageregistryapi.ImageRegistrySpec

	// TODO(legion): Add real bootstrap based on global ConfigMap or something.
	dc := &appsapi.DeploymentConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsapi.SchemeGroupVersion.String(),
			Kind:       "DeploymentConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName(h.params.Deployment.Namespace),
			Namespace: h.params.Deployment.Namespace,
		},
	}
	err = sdk.Get(dc)
	if errors.IsNotFound(err) {
		spec = imageregistryapi.ImageRegistrySpec{
			OperatorSpec: operatorapi.OperatorSpec{
				ManagementState: operatorapi.Managed,
				Version:         "none",
			},
			Storage:  imageregistryapi.ImageRegistryConfigStorage{},
			Replicas: 1,
		}
	} else if err != nil {
		return nil, fmt.Errorf("unable to check if the deployment already exists: %s", err)
	} else {
		logrus.Infof("adapting the existing deployment config...")
		var tlsSecret *corev1.Secret
		spec, tlsSecret, err = migration.NewImageRegistrySpecFromDeploymentConfig(dc, dependency.NewNamespacedResources(dc.ObjectMeta.Namespace))
		if err != nil {
			return nil, fmt.Errorf("unable to adapt the existing deployment config: %s", err)
		}
		if tlsSecret != nil {
			tlsSecret.TypeMeta = metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "Secret",
			}
			tlsSecret.ObjectMeta = metav1.ObjectMeta{
				Name:      dc.ObjectMeta.Name + "-tls",
				Namespace: dc.ObjectMeta.Namespace,
			}
			err = sdk.Create(tlsSecret)
			// TODO: it might already exist
			if err != nil {
				return nil, fmt.Errorf("unable to create the tls secret: %s", err)
			}
		}
	}

	logrus.Infof("generating registry custom resource")

	cr := &imageregistryapi.ImageRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapi.SchemeGroupVersion.String(),
			Kind:       "ImageRegistry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       resourceName(h.params.Deployment.Namespace),
			Namespace:  h.params.Deployment.Namespace,
			Finalizers: []string{parameters.ImageRegistryOperatorResourceFinalizer},
		},
		Spec:   spec,
		Status: imageregistryapi.ImageRegistryStatus{},
	}

	if len(cr.Spec.HTTPSecret) == 0 {
		var secretBytes [randomSecretSize]byte
		if _, err := rand.Read(secretBytes[:]); err != nil {
			return nil, fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
		}
		cr.Spec.HTTPSecret = fmt.Sprintf("%x", string(secretBytes[:]))
	}

	driver, err := storage.NewDriver(&cr.Spec.Storage)
	if err != nil {
		if err != storage.ErrStorageNotConfigured {
			return nil, err
		}
		cr.Status.Conditions = append(cr.Status.Conditions, operatorapi.OperatorCondition{
			Type:               operatorapi.OperatorStatusTypeAvailable,
			Status:             operatorapi.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "Bootstrap",
			Message:            err.Error(),
		})
	} else {
		err = driver.CompleteConfiguration()
		if err != nil {
			return nil, err
		}
	}

	return cr, sdk.Create(cr)
}
