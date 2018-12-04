package resource

import (
	"fmt"

	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/golang/glog"
	routeapi "github.com/openshift/api/route/v1"
	routeset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

var _ Templator = &generatorRoute{}

type generatorRoute struct {
	BaseTemplator
	Hostname   string
	SecretName string
	client     *routeset.RouteV1Client
}

func makeRoute(name string, hostname string, secretName string, g *Generator, cr *regopapi.ImageRegistry) (Templator, error) {
	client, err := routeset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	return &generatorRoute{
		BaseTemplator: BaseTemplator{
			Name:      name,
			Namespace: g.params.Deployment.Namespace,
			Strategy:  strategy.Override{},
			Generator: g,
		},
		Hostname:   hostname,
		SecretName: secretName,
		client:     client,
	}, nil
}

func (gr *generatorRoute) Expected() (runtime.Object, error) {
	r := &routeapi.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: routeapi.SchemeGroupVersion.String(),
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        gr.Name,
			Namespace:   gr.Namespace,
			Annotations: gr.Annotations,
		},
		Spec: routeapi.RouteSpec{
			Host: gr.Hostname,
			To: routeapi.RouteTargetReference{
				Kind: "Service",
				Name: gr.Generator.params.Service.Name,
			},
		},
	}

	r.Spec.TLS = &routeapi.TLSConfig{}

	// TLS certificates are served by the front end of the router, so they must be configured into the route,
	// otherwise the router's default certificate will be used for TLS termination.
	if gr.Generator.ImageRegistry.Spec.TLS {
		r.Spec.TLS.Termination = routeapi.TLSTerminationReencrypt
	} else {
		r.Spec.TLS.Termination = routeapi.TLSTerminationEdge
	}

	if len(gr.SecretName) > 0 {
		secret, err := gr.Generator.getSecret(gr.SecretName, gr.Namespace)
		if err != nil {
			return nil, err
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

	addOwnerRefToObject(r, asOwner(gr.Generator.ImageRegistry))

	return r, nil
}

func (gr *generatorRoute) Get() (runtime.Object, error) {
	return gr.client.Routes(gr.Namespace).Get(gr.Name, metav1.GetOptions{})
}

func (gr *generatorRoute) Create() error {
	tmpl, err := gr.Expected()
	if err != nil {
		return err
	}
	n := tmpl.(*routeapi.Route)

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gr.GetTemplateName(), err)
	}

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gr.client.Routes(gr.Namespace).Create(n)
	return err
}

func (gr *generatorRoute) Update(current runtime.Object) error {
	tmpl, err := gr.Expected()
	if err != nil {
		return err
	}

	dgst, err := Checksum(tmpl)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", gr.GetTemplateName(), err)
	}

	currentMeta, err := kmeta.Accessor(current)
	if err != nil {
		return fmt.Errorf("unable to get meta accessor for current object %s: %s", gr.GetTemplateName(), err)
	}

	curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
	if ok {
		if dgst == curdgst {
			glog.V(1).Infof("object has not changed: %s", gr.GetTemplateName())
			return nil
		}
	}

	updated, err := gr.Strategy.Apply(current, tmpl)
	if err != nil {
		return err
	}

	n := updated.(*routeapi.Route)

	annotations := n.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[parameters.ChecksumOperatorAnnotation] = dgst
	n.SetAnnotations(annotations)

	_, err = gr.client.Routes(gr.Namespace).Update(n)
	return err
}

func (gr *generatorRoute) Delete(opts *metav1.DeleteOptions) error {
	return gr.client.Routes(gr.Namespace).Delete(gr.Name, opts)
}

func (g *Generator) getRouteGenerators(cr *regopapi.ImageRegistry) map[string]ResourceGenerator {
	ret := map[string]ResourceGenerator{}

	if cr.Spec.DefaultRoute {
		defaultName := cr.Name + "-default-route"

		ret[defaultName] = func(g *Generator, cr *regopapi.ImageRegistry) (Templator, error) {
			return makeRoute(defaultName, "", "", g, cr)
		}
	}

	for i := range cr.Spec.Routes {
		ret[cr.Spec.Routes[i].Name] = func(g *Generator, cr *regopapi.ImageRegistry) (Templator, error) {
			return makeRoute(cr.Spec.Routes[i].Name, cr.Spec.Routes[i].Hostname, cr.Spec.Routes[i].SecretName, g, cr)
		}
	}

	return ret
}
