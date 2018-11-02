package operator

import (
	"crypto/rand"
	"fmt"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	operatorapi "github.com/openshift/api/operator/v1alpha1"
	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

// randomSecretSize is the number of random bytes to generate if no secret
// was specified.
const randomSecretSize = 64

func (h *Handler) Bootstrap() (*regopapi.ImageRegistry, error) {
	crList := &regopapi.ImageRegistryList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: regopapi.SchemeGroupVersion.String(),
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

	logrus.Infof("generating registry custom resource")

	// TODO(legion): Add real bootstrap based on global ConfigMap or something.
	cr := &regopapi.ImageRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: regopapi.SchemeGroupVersion.String(),
			Kind:       "ImageRegistry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "image-registry",
			Namespace:  h.params.Deployment.Namespace,
			Finalizers: []string{parameters.ImageRegistryOperatorResourceFinalizer},
		},
		Spec: regopapi.ImageRegistrySpec{
			OperatorSpec: operatorapi.OperatorSpec{
				ManagementState: operatorapi.Managed,
				Version:         "none",
			},
			Storage:  regopapi.ImageRegistryConfigStorage{},
			Replicas: 1,
		},
		Status: regopapi.ImageRegistryStatus{},
	}

	if len(cr.Spec.HTTPSecret) == 0 {
		var secretBytes [randomSecretSize]byte
		if _, err := rand.Read(secretBytes[:]); err != nil {
			return nil, fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
		}
		cr.Spec.HTTPSecret = fmt.Sprintf("%x", string(secretBytes[:]))
	}

	driver, err := storage.NewDriver(cr.Name, h.params.Deployment.Namespace, &cr.Spec.Storage)
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
