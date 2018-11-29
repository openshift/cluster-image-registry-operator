package operator

import (
	"fmt"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func appendFinalizer(cr *regopapi.ImageRegistry, modified *bool) {
	for i := range cr.ObjectMeta.Finalizers {
		if cr.ObjectMeta.Finalizers[i] == parameters.ImageRegistryOperatorResourceFinalizer {
			return
		}
	}

	cr.ObjectMeta.Finalizers = append(cr.ObjectMeta.Finalizers, parameters.ImageRegistryOperatorResourceFinalizer)
	*modified = true
}

func verifyResource(cr *regopapi.ImageRegistry, p *parameters.Globals) error {
	if cr.Spec.Replicas < 0 {
		return fmt.Errorf("replicas must be greater than or equal to 0")
	}

	names := map[string]struct{}{
		cr.ObjectMeta.Name + "-default-route": {},
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
