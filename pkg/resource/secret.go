package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

var _ Mutator = &generatorSecret{}

type generatorSecret struct {
	lister    corelisters.SecretNamespaceLister
	client    coreset.CoreV1Interface
	driver    storage.Driver
	name      string
	namespace string
}

func newGeneratorSecret(lister corelisters.SecretNamespaceLister, client coreset.CoreV1Interface, driver storage.Driver, params *parameters.Globals, cr *imageregistryv1.Config) *generatorSecret {
	return &generatorSecret{
		lister:    lister,
		client:    client,
		driver:    driver,
		name:      defaults.ImageRegistryPrivateConfiguration,
		namespace: params.Deployment.Namespace,
	}
}

func (gs *generatorSecret) Type() runtime.Object {
	return &corev1.Secret{}
}

func (gs *generatorSecret) GetGroup() string {
	return corev1.GroupName
}

func (gs *generatorSecret) GetResource() string {
	return "secrets"
}

func (gs *generatorSecret) GetNamespace() string {
	return gs.namespace
}

func (gs *generatorSecret) GetName() string {
	return gs.name
}

func (gs *generatorSecret) expected() (runtime.Object, error) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gs.GetName(),
			Namespace: gs.GetNamespace(),
		},
	}

	data, err := gs.driver.Secrets()
	if err != nil {
		return nil, err
	}

	sec.StringData = data

	return sec, nil
}

func (gs *generatorSecret) Get() (runtime.Object, error) {
	return gs.lister.Get(gs.GetName())
}

func (gs *generatorSecret) Create() (runtime.Object, error) {
	return commonCreate(gs, func(obj runtime.Object) (runtime.Object, error) {
		return gs.client.Secrets(gs.GetNamespace()).Create(obj.(*corev1.Secret))
	})
}

func (gs *generatorSecret) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gs, o, func(obj runtime.Object) (runtime.Object, error) {
		return gs.client.Secrets(gs.GetNamespace()).Update(obj.(*corev1.Secret))
	})
}

func (gs *generatorSecret) Delete(opts *metav1.DeleteOptions) error {
	return gs.client.Secrets(gs.GetNamespace()).Delete(gs.GetName(), opts)
}

func (g *generatorSecret) Owned() bool {
	return true
}
