package resource

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	kcorelisters "k8s.io/client-go/listers/core/v1"

	configapi "github.com/openshift/api/config/v1"
	configset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var _ Mutator = &generatorImageConfig{}

type generatorImageConfig struct {
	configLister  configlisters.ImageLister
	routeLister   routelisters.RouteNamespaceLister
	serviceLister kcorelisters.ServiceNamespaceLister
	configClient  configset.ConfigV1Interface
	name          string
	namespace     string
	serviceName   string
}

func newGeneratorImageConfig(configLister configlisters.ImageLister, routeLister routelisters.RouteNamespaceLister, serviceLister kcorelisters.ServiceNamespaceLister, configClient configset.ConfigV1Interface, params *parameters.Globals) *generatorImageConfig {
	return &generatorImageConfig{
		configLister:  configLister,
		routeLister:   routeLister,
		serviceLister: serviceLister,
		configClient:  configClient,
		name:          params.ImageConfig.Name,
		namespace:     params.Deployment.Namespace,
		serviceName:   params.Service.Name,
	}
}

func (gic *generatorImageConfig) Type() runtime.Object {
	return &configapi.Image{}
}

func (gic *generatorImageConfig) GetGroup() string {
	return configapi.GroupName
}

func (gic *generatorImageConfig) GetResource() string {
	return "images"
}

func (gic *generatorImageConfig) GetNamespace() string {
	return ""
}

func (gic *generatorImageConfig) GetName() string {
	return gic.name
}

func (gic *generatorImageConfig) Get() (runtime.Object, error) {
	return gic.configLister.Get(gic.GetName())
}

func (gic *generatorImageConfig) objectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name: gic.GetName(),
	}
}

func (gic *generatorImageConfig) Create() (runtime.Object, error) {
	ic := &configapi.Image{
		ObjectMeta: gic.objectMeta(),
	}

	ic, err := gic.configClient.Images().Create(ic)
	if err != nil {
		return ic, err
	}

	externalHostnames, err := gic.getRouteHostnames()
	if err != nil {
		return ic, err
	}
	ic.Status.ExternalRegistryHostnames = externalHostnames

	internalHostnames, err := getServiceHostnames(gic.serviceLister, gic.serviceName)
	if err != nil {
		return ic, err
	}

	internalHostname := ""
	if len(internalHostnames) > 0 {
		internalHostname = internalHostnames[0]
	}

	ic.Status.InternalRegistryHostname = internalHostname

	// Create strips status fields, so need to explicitly set status separately
	return gic.configClient.Images().UpdateStatus(ic)
}

func (gic *generatorImageConfig) Update(o runtime.Object) (runtime.Object, bool, error) {
	ic := o.(*configapi.Image)

	externalHostnames, err := gic.getRouteHostnames()
	if err != nil {
		return o, false, err
	}

	modified := false
	if !reflect.DeepEqual(externalHostnames, ic.Status.ExternalRegistryHostnames) {
		ic.Status.ExternalRegistryHostnames = externalHostnames
		modified = true
	}

	internalHostnames, err := getServiceHostnames(gic.serviceLister, gic.serviceName)
	if err != nil {
		return o, false, err
	}

	internalHostname := ""
	if len(internalHostnames) > 0 {
		internalHostname = internalHostnames[0]
	}

	if ic.Status.InternalRegistryHostname != internalHostname {
		ic.Status.InternalRegistryHostname = internalHostname
		modified = true
	}

	if !modified {
		return o, false, nil
	}

	n, err := gic.configClient.Images().UpdateStatus(ic)
	return n, err == nil, err
}

func (gic *generatorImageConfig) Delete(opts *metav1.DeleteOptions) error {
	return gic.configClient.Images().Delete(gic.GetName(), opts)
}

func (g *generatorImageConfig) Owned() bool {
	// the registry operator can create and contribute to the imageconfig, but it doesn't own it.
	return false
}

func (gic *generatorImageConfig) getRouteHostnames() ([]string, error) {
	var externalHostnames []string

	routes, err := gic.routeLister.List(labels.Everything())
	if err != nil {
		return []string{}, err
	}
	defaultHost := ""
	for _, route := range routes {
		if !RouteIsCreatedByOperator(route) {
			continue
		}
		for _, ingress := range route.Status.Ingress {
			hostname := ingress.Host
			if len(hostname) == 0 {
				continue
			}
			if strings.HasPrefix(hostname, defaults.RouteName+"-"+gic.namespace) {
				defaultHost = hostname
			} else {
				externalHostnames = append(externalHostnames, hostname)
			}
		}
	}

	// ensure a stable order for these values so we don't cause flapping in the downstream
	// controllers that watch this array
	sort.Strings(externalHostnames)
	// make sure the default route hostname comes first in the list because the first entry will be used
	// as the public repository hostname by the cluster configuration
	if len(defaultHost) > 0 {
		externalHostnames = append([]string{defaultHost}, externalHostnames...)
	}

	return externalHostnames, nil
}

func getServiceHostnames(serviceLister kcorelisters.ServiceNamespaceLister, serviceName string) ([]string, error) {
	svc, err := serviceLister.Get(serviceName)
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	port := ""
	if svc.Spec.Ports[0].Port != 443 {
		port = fmt.Sprintf(":%d", svc.Spec.Ports[0].Port)
	}
	return []string{
		fmt.Sprintf("%s.%s.svc%s", svc.Name, svc.Namespace, port),
		fmt.Sprintf("%s.%s.svc.cluster.local%s", svc.Name, svc.Namespace, port),
	}, nil
}
