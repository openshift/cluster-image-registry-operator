package operator

import (
	"crypto/rand"
	"fmt"

	"github.com/sirupsen/logrus"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

// randomSecretSize is the number of random bytes to generate if no secret
// was specified.
const randomSecretSize = 64

func completeResource(cr *regopapi.OpenShiftDockerRegistry, p *parameters.Globals, modified *bool) error {
	if len(cr.Spec.HTTPSecret) == 0 {
		var secretBytes [randomSecretSize]byte
		if _, err := rand.Read(secretBytes[:]); err != nil {
			return fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
		}
		cr.Spec.HTTPSecret = fmt.Sprintf("%x", string(secretBytes[:]))

		*modified = true
		logrus.Warn("No HTTP secret provided - generated random secret")
	}

	names := map[string]struct{}{
		p.DefaultRoute.Name: {},
	}

	for _, routeSpec := range cr.Spec.Routes {
		_, found := names[routeSpec.Name]
		if found {
			return fmt.Errorf("duplication of names has been detected in the additional routes")
		}
		names[routeSpec.Name] = struct{}{}
	}

	driver, err := storage.NewDriver(&cr.Spec.Storage)
	if err != nil {
		return err
	}

	err = driver.CompleteConfiguration()
	if err != nil {
		return err
	}

	return nil
}
