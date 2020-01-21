package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var _ Mutator = &generatorPrunerServiceAccount{}

type generatorPrunerServiceAccount struct {
	lister corelisters.ServiceAccountNamespaceLister
	client coreset.CoreV1Interface
}

func newGeneratorPrunerServiceAccount(lister corelisters.ServiceAccountNamespaceLister, client coreset.CoreV1Interface, params *parameters.Globals) *generatorPrunerServiceAccount {
	return &generatorPrunerServiceAccount{
		lister: lister,
		client: client,
	}
}

func (gsa *generatorPrunerServiceAccount) Type() runtime.Object {
	return &corev1.ServiceAccount{}
}

func (gsa *generatorPrunerServiceAccount) GetGroup() string {
	return corev1.GroupName
}

func (gsa *generatorPrunerServiceAccount) GetResource() string {
	return "serviceaccounts"
}

func (gsa *generatorPrunerServiceAccount) GetNamespace() string {
	return "openshift-image-registry"
}

func (gsa *generatorPrunerServiceAccount) GetName() string {
	return "pruner"
}

func (gsa *generatorPrunerServiceAccount) expected() (runtime.Object, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gsa.GetName(),
			Namespace: gsa.GetNamespace(),
		},
	}

	return sa, nil
}

func (gsa *generatorPrunerServiceAccount) Get() (runtime.Object, error) {
	return gsa.lister.Get(gsa.GetName())
}

func (gsa *generatorPrunerServiceAccount) Create() (runtime.Object, error) {
	return commonCreate(gsa, func(obj runtime.Object) (runtime.Object, error) {
		return gsa.client.ServiceAccounts(gsa.GetNamespace()).Create(obj.(*corev1.ServiceAccount))
	})
}

func (gsa *generatorPrunerServiceAccount) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gsa, o, func(obj runtime.Object) (runtime.Object, error) {
		return gsa.client.ServiceAccounts(gsa.GetNamespace()).Update(obj.(*corev1.ServiceAccount))
	})
}

func (gsa *generatorPrunerServiceAccount) Delete(opts *metav1.DeleteOptions) error {
	return gsa.client.ServiceAccounts(gsa.GetNamespace()).Delete(gsa.GetName(), opts)
}

func (g *generatorPrunerServiceAccount) Owned() bool {
	return true
}
