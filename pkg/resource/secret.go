package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

var _ Mutator = &generatorSecret{}

type generatorSecret struct {
	lister    corelisters.SecretNamespaceLister
	client    coreset.CoreV1Interface
	name      string
	namespace string
	owner     metav1.OwnerReference
}

func newGeneratorSecret(lister corelisters.SecretNamespaceLister, client coreset.CoreV1Interface, params *parameters.Globals, cr *regopapi.ImageRegistry) *generatorSecret {
	return &generatorSecret{
		lister:    lister,
		client:    client,
		name:      cr.Name + "-private-configuration",
		namespace: params.Deployment.Namespace,
		owner:     asOwner(cr),
	}
}

func (gs *generatorSecret) Type() runtime.Object {
	return &corev1.Secret{}
}

func (gs *generatorSecret) GetNamespace() string {
	return gs.namespace
}

func (gs *generatorSecret) GetName() string {
	return gs.name
}

func (gs *generatorSecret) expected() (*corev1.Secret, error) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gs.GetName(),
			Namespace: gs.GetNamespace(),
		},
	}

	addOwnerRefToObject(sec, gs.owner)

	return sec, nil
}

func (gs *generatorSecret) Get() (runtime.Object, error) {
	return gs.lister.Get(gs.GetName())
}

func (gs *generatorSecret) Create() error {
	sec, err := gs.expected()
	if err != nil {
		return err
	}

	_, err = gs.client.Secrets(gs.GetNamespace()).Create(sec)
	return err
}

func (gs *generatorSecret) Update(o runtime.Object) (bool, error) {
	sec := o.(*corev1.Secret)

	n, err := gs.expected()
	if err != nil {
		return false, err
	}

	updated := strategy.Metadata(&sec.ObjectMeta, &n.ObjectMeta)
	if !updated {
		return false, nil
	}

	_, err = gs.client.Secrets(gs.GetNamespace()).Update(sec)
	return true, err
}

func (gs *generatorSecret) Delete(opts *metav1.DeleteOptions) error {
	return gs.client.Secrets(gs.GetNamespace()).Delete(gs.GetName(), opts)
}
