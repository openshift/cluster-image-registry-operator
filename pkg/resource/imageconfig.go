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

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
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
	owner         metav1.OwnerReference
}

func newGeneratorImageConfig(configLister configlisters.ImageLister, routeLister routelisters.RouteNamespaceLister, serviceLister kcorelisters.ServiceNamespaceLister, configClient configset.ConfigV1Interface, params *parameters.Globals, cr *imageregistryv1.Config) *generatorImageConfig {
	return &generatorImageConfig{
		configLister:  configLister,
		routeLister:   routeLister,
		serviceLister: serviceLister,
		configClient:  configClient,
		name:          params.ImageConfig.Name,
		namespace:     params.Deployment.Namespace,
		serviceName:   params.Service.Name,
		owner:         asOwner(cr),
	}
}

func (gic *generatorImageConfig) Type() runtime.Object {
	return &configapi.Image{}
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

func (gic *generatorImageConfig) Create() error {
	ic := &configapi.Image{
		ObjectMeta: gic.objectMeta(),
	}

	ic, err := gic.configClient.Images().Create(ic)
	if err != nil {
		return err
	}

	externalHostnames, err := gic.getRouteHostnames()
	if err != nil {
		return err
	}
	ic.Status.ExternalRegistryHostnames = externalHostnames

	internalHostname, err := getServiceHostname(gic.serviceLister, gic.serviceName)
	if err != nil {
		return err
	}
	ic.Status.InternalRegistryHostname = internalHostname

	// Create strips status fields, so need to explicitly set status separately
	_, err = gic.configClient.Images().UpdateStatus(ic)
	return err
}

func (gic *generatorImageConfig) Update(o runtime.Object) (bool, error) {
	ic := o.(*configapi.Image)

	externalHostnames, err := gic.getRouteHostnames()
	if err != nil {
		return false, err
	}

	modified := false
	if !reflect.DeepEqual(externalHostnames, ic.Status.ExternalRegistryHostnames) {
		ic.Status.ExternalRegistryHostnames = externalHostnames
		modified = true
	}

	internalHostname, err := getServiceHostname(gic.serviceLister, gic.serviceName)
	if err != nil {
		return false, err
	}

	if ic.Status.InternalRegistryHostname != internalHostname {
		ic.Status.InternalRegistryHostname = internalHostname
		modified = true
	}

	if !modified {
		return false, nil
	}

	_, err = gic.configClient.Images().UpdateStatus(ic)
	return err == nil, err
}

func (gic *generatorImageConfig) Delete(opts *metav1.DeleteOptions) error {
	return gic.configClient.Images().Delete(gic.GetName(), opts)
}

func (gic *generatorImageConfig) getRouteHostnames() ([]string, error) {
	var externalHostnames []string

	routes, err := gic.routeLister.List(labels.Everything())
	if err != nil {
		return []string{}, err
	}
	defaultHost := ""
	for _, route := range routes {
		routeOwner := metav1.GetControllerOf(route)

		// ignore routes that weren't created by the registry operator
		if routeOwner == nil || routeOwner.UID != gic.owner.UID {
			continue
		}
		for _, ingress := range route.Status.Ingress {
			hostname := ingress.Host
			if len(hostname) == 0 {
				continue
			}
			if strings.HasPrefix(hostname, imageregistryv1.DefaultRouteName+"-"+gic.namespace) {
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

func getServiceHostname(serviceLister kcorelisters.ServiceNamespaceLister, serviceName string) (string, error) {
	svc, err := serviceLister.Get(serviceName)
	if errors.IsNotFound(err) {
		return "", nil
	}
	if svc == nil || err != nil {
		return "", err
	}
	svcHostname := fmt.Sprintf("%s.%s.svc:%d", svc.Name, svc.Namespace, svc.Spec.Ports[0].Port)
	return svcHostname, nil
}
