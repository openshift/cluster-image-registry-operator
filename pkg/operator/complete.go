package operator

import (
	"crypto/rand"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
)

// randomSecretSize is the number of random bytes to generate if no secret
// was specified.
const randomSecretSize = 64

func completeResource(cr *v1alpha1.OpenShiftDockerRegistry, modified *bool) error {
	if len(cr.Spec.HTTPSecret) == 0 {
		var secretBytes [randomSecretSize]byte
		if _, err := rand.Read(secretBytes[:]); err != nil {
			return fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
		}
		cr.Spec.HTTPSecret = string(secretBytes[:])

		*modified = true
		logrus.Warn("No HTTP secret provided - generated random secret")
	}

	if cr.Spec.TLS == nil {
		boolvar := true
		cr.Spec.TLS = &boolvar

		*modified = true
		logrus.Warn("No TLS specified - enabled by default")
	}

	return nil
}
