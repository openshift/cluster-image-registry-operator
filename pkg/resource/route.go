package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corelisters "k8s.io/client-go/listers/core/v1"

	routeapi "github.com/openshift/api/route/v1"
	routeset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

const RouteOwnerAnnotation = "imageregistry.openshift.io"

func RouteIsCreatedByOperator(route *routeapi.Route) bool {
	_, ok := route.Annotations[RouteOwnerAnnotation]
	return ok
}

var _ Mutator = &generatorRoute{}

type generatorRoute struct {
	lister       routelisters.RouteNamespaceLister
	secretLister corelisters.SecretNamespaceLister
	client       routeset.RouteV1Interface
	namespace    string
	serviceName  string
	route        imageregistryv1.ImageRegistryConfigRoute
}

func newGeneratorRoute(lister routelisters.RouteNamespaceLister, secretLister corelisters.SecretNamespaceLister, client routeset.RouteV1Interface, params *parameters.Globals, cr *imageregistryv1.Config, route imageregistryv1.ImageRegistryConfigRoute) *generatorRoute {
	return &generatorRoute{
		lister:       lister,
		secretLister: secretLister,
		client:       client,
		namespace:    params.Deployment.Namespace,
		serviceName:  params.Service.Name,
		route:        route,
	}
}

func (gr *generatorRoute) Type() runtime.Object {
	return &routeapi.Route{}
}

func (gr *generatorRoute) GetGroup() string {
	return routeapi.GroupName
}

func (gr *generatorRoute) GetResource() string {
	return "routes"
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
			Name:        gr.GetName(),
			Namespace:   gr.GetNamespace(),
			Annotations: map[string]string{RouteOwnerAnnotation: "true"},
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
	r.Spec.TLS.Termination = routeapi.TLSTerminationReencrypt

	if len(gr.route.SecretName) > 0 {
		secret, err := gr.secretLister.Get(gr.route.SecretName)
		if err != nil {
			return nil, err
		}
		if v, ok := secret.Data["tls.crt"]; ok {
			r.Spec.TLS.Certificate = string(v)
		}
		if v, ok := secret.Data["tls.key"]; ok {
			r.Spec.TLS.Key = string(v)
		}
		if v, ok := secret.Data["tls.cacrt"]; ok {
			r.Spec.TLS.CACertificate = string(v)
		}
	}
	return r, nil
}

func (gr *generatorRoute) Get() (runtime.Object, error) {
	return gr.lister.Get(gr.GetName())
}

func (gr *generatorRoute) Create() (runtime.Object, error) {
	return commonCreate(gr, func(obj runtime.Object) (runtime.Object, error) {
		return gr.client.Routes(gr.GetNamespace()).Create(obj.(*routeapi.Route))
	})
}

func (gr *generatorRoute) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gr, o, func(obj runtime.Object) (runtime.Object, error) {
		return gr.client.Routes(gr.GetNamespace()).Update(obj.(*routeapi.Route))
	})
}

func (gr *generatorRoute) Delete(opts *metav1.DeleteOptions) error {
	return gr.client.Routes(gr.GetNamespace()).Delete(gr.GetName(), opts)
}

func (g *generatorRoute) Owned() bool {
	return true
}
