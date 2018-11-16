package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routeapi "github.com/openshift/api/route/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func makeDefaultRoute(cr *regopapi.ImageRegistry, p *parameters.Globals) (Template, error) {
	r := &routeapi.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: routeapi.SchemeGroupVersion.String(),
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.ObjectMeta.Name + "-default-route",
			Namespace: p.Deployment.Namespace,
		},
		Spec: routeapi.RouteSpec{
			To: routeapi.RouteTargetReference{
				Kind: "Service",
				Name: p.Service.Name,
			},
		},
	}

	r.Spec.TLS = &routeapi.TLSConfig{}

	// TLS certificates are served by the front end of the router, so they must be configured into the route,
	// otherwise the router's default certificate will be used for TLS termination.
	if cr.Spec.TLS {
		r.Spec.TLS.Termination = routeapi.TLSTerminationReencrypt
	} else {
		r.Spec.TLS.Termination = routeapi.TLSTerminationEdge
	}

	addOwnerRefToObject(r, asOwner(cr))
	return Template{
		Object:   r,
		Strategy: strategy.Override{},
	}, nil
}

func makeRoute(cr *regopapi.ImageRegistry, route *regopapi.ImageRegistryConfigRoute, p *parameters.Globals) (Template, error) {
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
				Name: p.Service.Name,
			},
		},
	}

	r.Spec.TLS = &routeapi.TLSConfig{}

	if cr.Spec.TLS {
		r.Spec.TLS.Termination = routeapi.TLSTerminationReencrypt
	} else {
		r.Spec.TLS.Termination = routeapi.TLSTerminationEdge
	}

	if len(route.SecretName) > 0 {
		secret, err := getSecret(route.SecretName, p.Deployment.Namespace)
		if err != nil {
			return Template{}, err
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

func GetRouteGenerators(cr *regopapi.ImageRegistry, p *parameters.Globals) map[string]Generator {
	ret := map[string]Generator{}

	if cr.Spec.DefaultRoute {
		ret[cr.ObjectMeta.Name+"-default-route"] = makeDefaultRoute
	}

	for i := range cr.Spec.Routes {
		ret[cr.Spec.Routes[i].Name] = func(o *regopapi.ImageRegistry, p *parameters.Globals) (Template, error) {
			return makeRoute(o, &o.Spec.Routes[i], p)
		}
	}

	return ret
}
