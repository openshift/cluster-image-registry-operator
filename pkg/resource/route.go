package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corelisters "k8s.io/client-go/listers/core/v1"

	routeapi "github.com/openshift/api/route/v1"
	routeset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var _ Mutator = &generatorRoute{}

type generatorRoute struct {
	lister       routelisters.RouteNamespaceLister
	secretLister corelisters.SecretNamespaceLister
	client       routeset.RouteV1Interface
	namespace    string
	serviceName  string
	tls          bool
	owner        metav1.OwnerReference
	route        regopapi.ImageRegistryConfigRoute
}

func newGeneratorRoute(lister routelisters.RouteNamespaceLister, secretLister corelisters.SecretNamespaceLister, client routeset.RouteV1Interface, params *parameters.Globals, cr *regopapi.ImageRegistry, route regopapi.ImageRegistryConfigRoute) *generatorRoute {
	return &generatorRoute{
		lister:       lister,
		secretLister: secretLister,
		client:       client,
		namespace:    params.Deployment.Namespace,
		serviceName:  params.Service.Name,
		tls:          cr.Spec.TLS,
		owner:        asOwner(cr),
		route:        route,
	}
}

func (gr *generatorRoute) Type() runtime.Object {
	return &routeapi.Route{}
}

func (gr *generatorRoute) GetNamespace() string {
	return gr.namespace
}

func (gr *generatorRoute) GetName() string {
	return gr.route.Name
}

func (gr *generatorRoute) expected() (runtime.Object, error) {
	r := &routeapi.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gr.GetName(),
			Namespace: gr.GetNamespace(),
		},
		Spec: routeapi.RouteSpec{
			Host: gr.route.Hostname,
			To: routeapi.RouteTargetReference{
				Kind: "Service",
				Name: gr.serviceName,
			},
		},
	}

	r.Spec.TLS = &routeapi.TLSConfig{}
	if gr.tls {
		r.Spec.TLS.Termination = routeapi.TLSTerminationReencrypt
	} else {
		r.Spec.TLS.Termination = routeapi.TLSTerminationEdge
	}

	if len(gr.route.SecretName) > 0 {
		secret, err := gr.secretLister.Get(gr.route.SecretName)
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

	addOwnerRefToObject(r, gr.owner)

	return r, nil
}

func (gr *generatorRoute) Get() (runtime.Object, error) {
	return gr.lister.Get(gr.GetName())
}

func (gr *generatorRoute) Create() error {
	return commonCreate(gr, func(obj runtime.Object) (runtime.Object, error) {
		return gr.client.Routes(gr.GetNamespace()).Create(obj.(*routeapi.Route))
	})
}

func (gr *generatorRoute) Update(o runtime.Object) (bool, error) {
	return commonUpdate(gr, o, func(obj runtime.Object) (runtime.Object, error) {
		return gr.client.Routes(gr.GetNamespace()).Update(obj.(*routeapi.Route))
	})
}

func (gr *generatorRoute) Delete(opts *metav1.DeleteOptions) error {
	return gr.client.Routes(gr.GetNamespace()).Delete(gr.GetName(), opts)
}
