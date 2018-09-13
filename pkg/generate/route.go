package generate

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routeapi "github.com/openshift/api/route/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func DefaultRoute(cr *regopapi.OpenShiftDockerRegistry, p *parameters.Globals) Template {
	r := &routeapi.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: routeapi.SchemeGroupVersion.String(),
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.DefaultRoute.Name,
			Namespace: p.Deployment.Namespace,
		},
		Spec: routeapi.RouteSpec{
			To: routeapi.RouteTargetReference{
				Kind: "Service",
				Name: p.Deployment.Name,
			},
		},
	}
	if cr.Spec.TLS {
		// TLS certificates are served by the front end of the router, so they must be configured into the route,
		// otherwise the router's default certificate will be used for TLS termination.
		r.Spec.TLS = &routeapi.TLSConfig{
			Termination: routeapi.TLSTerminationReencrypt,
		}
	}
	addOwnerRefToObject(r, asOwner(cr))
	return Template{
		Object:   r,
		Strategy: strategy.Override{},
	}
}

func Route(cr *regopapi.OpenShiftDockerRegistry, route *regopapi.OpenShiftDockerRegistryConfigRoute, p *parameters.Globals) (Template, error) {
	r := &routeapi.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: routeapi.SchemeGroupVersion.String(),
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      route.Name,
			Namespace: p.Deployment.Namespace,
		},
		Spec: routeapi.RouteSpec{
			Host: route.Hostname,
			To: routeapi.RouteTargetReference{
				Kind: "Service",
				Name: p.Deployment.Name,
			},
		},
	}
	if cr.Spec.TLS {
		secret, err := getSecret(route.SecretName, p.Deployment.Name)
		if err != nil {
			return Template{}, err
		}
		r.Spec.TLS = &routeapi.TLSConfig{
			Termination: routeapi.TLSTerminationReencrypt,
		}
		if v, ok := secret.StringData["tls.crt"]; ok {
			r.Spec.TLS.Certificate = v
		}
		if v, ok := secret.StringData["tls.key"]; ok {
			r.Spec.TLS.Key = v
		}
		if v, ok := secret.StringData["tls.cacrt"]; ok {
			r.Spec.TLS.CACertificate = v
		}
	}
	addOwnerRefToObject(r, asOwner(cr))
	return Template{
		Object:   r,
		Strategy: strategy.Override{},
	}, nil
}
