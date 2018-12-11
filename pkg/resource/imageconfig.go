package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	configapi "github.com/openshift/api/config/v1"
	configset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var _ Mutator = &generatorImageConfig{}

type generatorImageConfig struct {
	lister   configlisters.ImageLister
	client   configset.ConfigV1Interface
	name     string
	hostname string
	owner    metav1.OwnerReference
}

func newGeneratorImageConfig(lister configlisters.ImageLister, client configset.ConfigV1Interface, params *parameters.Globals, cr *regopapi.ImageRegistry) *generatorImageConfig {
	return &generatorImageConfig{
		lister:   lister,
		client:   client,
		name:     params.ImageConfig.Name,
		hostname: cr.Status.InternalRegistryHostname,
		owner:    asOwner(cr),
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
	return gic.lister.Get(gic.GetName())
}

func (gic *generatorImageConfig) objectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name: gic.GetName(),
	}
}

func (gic *generatorImageConfig) Create() error {
	ic := &configapi.Image{
		ObjectMeta: gic.objectMeta(),
		Status: configapi.ImageStatus{
			InternalRegistryHostname: gic.hostname,
		},
	}

	_, err := gic.client.Images().Create(ic)
	return err
}

func (gic *generatorImageConfig) Update(o runtime.Object) (bool, error) {
	ic := o.(*configapi.Image)

	modified := false
	if ic.Status.InternalRegistryHostname != gic.hostname {
		ic.Status.InternalRegistryHostname = gic.hostname
		modified = true
	}
	if !modified {
		return false, nil
	}

	_, err := gic.client.Images().UpdateStatus(ic)
	return true, err
}

func (gic *generatorImageConfig) Delete(opts *metav1.DeleteOptions) error {
	return gic.client.Images().Delete(gic.GetName(), opts)
}
