package operator

import (
	"crypto/rand"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	operatorapi "github.com/openshift/api/operator/v1alpha1"
	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

// randomSecretSize is the number of random bytes to generate if no secret
// was specified.
const randomSecretSize = 64

func (h *Handler) bootstrap() error {
	// TODO(legion): Add real bootstrap based on global ConfigMap or something.

	cr := &regopapi.OpenShiftDockerRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: regopapi.SchemeGroupVersion.String(),
			Kind:       "OpenShiftDockerRegistry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry",
			Namespace: h.params.Deployment.Namespace,
		},
	}

	err := sdk.Get(cr)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get image-registry custom resource: %s", err)
		}
	} else {
		return nil
	}

	cr.Spec = regopapi.OpenShiftDockerRegistrySpec{
		OperatorSpec: operatorapi.OperatorSpec{
			ManagementState: operatorapi.Managed,
			Version:         "none",
			ImagePullSpec:   "docker.io/openshift/origin-docker-registry",
		},
		Storage: regopapi.OpenShiftDockerRegistryConfigStorage{
			Filesystem: &regopapi.OpenShiftDockerRegistryConfigStorageFilesystem{
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
			return fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
		}
		cr.Spec.HTTPSecret = fmt.Sprintf("%x", string(secretBytes[:]))
	}

	driver, err := storage.NewDriver(&cr.Spec.Storage)
	if err != nil {
		return err
	}

	err = driver.CompleteConfiguration()
	if err != nil {
		return err
	}

	return sdk.Create(cr)
}
