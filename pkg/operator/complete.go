package operator

import (
	"crypto/rand"
	"fmt"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

// randomSecretSize is the number of random bytes to generate
// for the http secret
const randomSecretSize = 64

func appendFinalizer(cr *imageregistryv1.Config) {
	for i := range cr.ObjectMeta.Finalizers {
		if cr.ObjectMeta.Finalizers[i] == defaults.ImageRegistryOperatorResourceFinalizer {
			return
		}
	}

	cr.ObjectMeta.Finalizers = append(cr.ObjectMeta.Finalizers, defaults.ImageRegistryOperatorResourceFinalizer)
}

func verifyResource(cr *imageregistryv1.Config) error {
	if cr.Spec.Replicas < 0 {
		return fmt.Errorf("replicas must be greater than or equal to 0")
	}

	names := map[string]struct{}{
		defaults.RouteName: {},
	}

	for _, routeSpec := range cr.Spec.Routes {
		_, found := names[routeSpec.Name]
		if found {
			return fmt.Errorf("duplication of names has been detected in the additional routes")
		}
		names[routeSpec.Name] = struct{}{}
	}

	return nil
}

func applyDefaults(cr *imageregistryv1.Config) error {
	if cr.Spec.HTTPSecret == "" {
		var secretBytes [randomSecretSize]byte
		if _, err := rand.Read(secretBytes[:]); err != nil {
			return fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
		}

		cr.Spec.HTTPSecret = fmt.Sprintf("%x", string(secretBytes[:]))
	}

	return nil
}
