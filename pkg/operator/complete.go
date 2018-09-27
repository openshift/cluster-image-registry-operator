package operator

import (
	"fmt"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func verifyResource(cr *regopapi.ImageRegistry, p *parameters.Globals) error {
	names := map[string]struct{}{
		p.DefaultRoute.Name: {},
	}

	for _, routeSpec := range cr.Spec.Routes {
		_, found := names[routeSpec.Name]
		if found {
			return fmt.Errorf("duplication of names has been detected in the additional routes")
		}
		if len(routeSpec.SecretName) == 0 {
			return fmt.Errorf("secretname must be specified for route: %s", routeSpec.Name)
		}
		names[routeSpec.Name] = struct{}{}
	}

	return nil
}
