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

var _ Mutator = &generatorServiceAccount{}

type generatorServiceAccount struct {
	lister    corelisters.ServiceAccountNamespaceLister
	client    coreset.CoreV1Interface
	name      string
	namespace string
	owner     metav1.OwnerReference
}

func newGeneratorServiceAccount(lister corelisters.ServiceAccountNamespaceLister, client coreset.CoreV1Interface, params *parameters.Globals, cr *regopapi.ImageRegistry) *generatorServiceAccount {
	return &generatorServiceAccount{
		lister:    lister,
		client:    client,
		name:      params.Pod.ServiceAccount,
		namespace: params.Deployment.Namespace,
		owner:     asOwner(cr),
	}
}

func (gsa *generatorServiceAccount) Type() interface{} {
	return &corev1.ServiceAccount{}
}

func (gsa *generatorServiceAccount) GetNamespace() string {
	return gsa.namespace
}

func (gsa *generatorServiceAccount) GetName() string {
	return gsa.name
}

func (gsa *generatorServiceAccount) expected() (*corev1.ServiceAccount, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gsa.GetName(),
			Namespace: gsa.GetNamespace(),
		},
	}

	addOwnerRefToObject(sa, gsa.owner)

	return sa, nil
}

func (gsa *generatorServiceAccount) Get() (runtime.Object, error) {
	return gsa.lister.Get(gsa.GetName())
}

func (gsa *generatorServiceAccount) Create() error {
	deploy, err := gsa.expected()
	if err != nil {
		return err
	}

	_, err = gsa.client.ServiceAccounts(gsa.GetNamespace()).Create(deploy)
	return err
}

func (gsa *generatorServiceAccount) Update(o runtime.Object) (bool, error) {
	sa := o.(*corev1.ServiceAccount)

	n, err := gsa.expected()
	if err != nil {
		return false, err
	}

	updated, err := strategy.Override(sa, n)
	if !updated || err != nil {
		return false, err
	}

	_, err = gsa.client.ServiceAccounts(gsa.GetNamespace()).Update(sa)
	return true, err
}

func (gsa *generatorServiceAccount) Delete(opts *metav1.DeleteOptions) error {
	return gsa.client.ServiceAccounts(gsa.GetNamespace()).Delete(gsa.GetName(), opts)
}
