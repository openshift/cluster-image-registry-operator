package operator

import (
	"fmt"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func appendFinalizer(cr *imageregistryv1.Config) {
	for i := range cr.ObjectMeta.Finalizers {
		if cr.ObjectMeta.Finalizers[i] == parameters.ImageRegistryOperatorResourceFinalizer {
			return
		}
	}

	cr.ObjectMeta.Finalizers = append(cr.ObjectMeta.Finalizers, parameters.ImageRegistryOperatorResourceFinalizer)
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
