package operator

import (
	"crypto/rand"
	"fmt"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	operatorapi "github.com/openshift/api/operator/v1alpha1"
	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

// randomSecretSize is the number of random bytes to generate if no secret
// was specified.
const randomSecretSize = 64

func (h *Handler) Bootstrap() (*regopapi.ImageRegistry, error) {
	// TODO(legion): Add real bootstrap based on global ConfigMap or something.

	cr := &regopapi.ImageRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: regopapi.SchemeGroupVersion.String(),
			Kind:       "ImageRegistry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry",
			Namespace: h.params.Deployment.Namespace,
		},
	}

	err := sdk.Get(cr)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get image-registry custom resource: %s", err)
		}
	} else {
		return cr, nil
	}

	logrus.Infof("generating registry custom resource")

	cr.Spec = regopapi.ImageRegistrySpec{
		OperatorSpec: operatorapi.OperatorSpec{
			ManagementState: operatorapi.Managed,
			Version:         "none",
			ImagePullSpec:   "docker.io/openshift/origin-docker-registry",
		},
		Storage: regopapi.ImageRegistryConfigStorage{
			Filesystem: &regopapi.ImageRegistryConfigStorageFilesystem{
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "image-registry-pvc",
					},
				},
			},
		},
		Replicas: 1,
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
		return nil, err
	}

	err = driver.CompleteConfiguration()
	if err != nil {
		return nil, err
	}

	return cr, sdk.Create(cr)
}
