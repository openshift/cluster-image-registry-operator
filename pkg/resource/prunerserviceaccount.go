package resource

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
)

var _ Mutator = &generatorPrunerServiceAccount{}

type generatorPrunerServiceAccount struct {
	lister corelisters.ServiceAccountNamespaceLister
	client coreset.CoreV1Interface
}

func newGeneratorPrunerServiceAccount(lister corelisters.ServiceAccountNamespaceLister, client coreset.CoreV1Interface) *generatorPrunerServiceAccount {
	return &generatorPrunerServiceAccount{
		lister: lister,
		client: client,
	}
}

func (gsa *generatorPrunerServiceAccount) Type() runtime.Object {
	return &corev1.ServiceAccount{}
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
		return gsa.client.ServiceAccounts(gsa.GetNamespace()).Create(
			context.TODO(), obj.(*corev1.ServiceAccount), metav1.CreateOptions{},
		)
	})
}

func (gsa *generatorPrunerServiceAccount) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gsa, o, func(obj runtime.Object) (runtime.Object, error) {
		return gsa.client.ServiceAccounts(gsa.GetNamespace()).Update(
			context.TODO(), obj.(*corev1.ServiceAccount), metav1.UpdateOptions{},
		)
	})
}

func (gsa *generatorPrunerServiceAccount) Delete(opts metav1.DeleteOptions) error {
	return gsa.client.ServiceAccounts(gsa.GetNamespace()).Delete(
		context.TODO(), gsa.GetName(), opts,
	)
}

func (g *generatorPrunerServiceAccount) Owned() bool {
	return true
}
