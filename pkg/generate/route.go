package generate

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routeapi "github.com/openshift/api/route/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func Route(cr *v1alpha1.OpenShiftDockerRegistry, p *parameters.Globals) Template {
	r := &routeapi.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: routeapi.SchemeGroupVersion.String(),
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Deployment.Name,
			Namespace: p.Deployment.Namespace,
		},
		Spec: routeapi.RouteSpec{
			Host: cr.Spec.Route.Hostname,
			To: routeapi.RouteTargetReference{
				Kind: "Service",
				Name: p.Deployment.Name,
			},
		},
	}
	if cr.Spec.TLS {
		r.Spec.TLS = &routeapi.TLSConfig{
			Termination: routeapi.TLSTerminationPassthrough,
		}
	}
	addOwnerRefToObject(r, asOwner(cr))
	return Template{
		Object:   r,
		Strategy: strategy.Override{},
	}
}
