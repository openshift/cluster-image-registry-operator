package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var _ Mutator = &generatorServiceAccount{}

type generatorServiceAccount struct {
	lister    corelisters.ServiceAccountNamespaceLister
	client    coreset.CoreV1Interface
	name      string
	namespace string
}

func newGeneratorServiceAccount(lister corelisters.ServiceAccountNamespaceLister, client coreset.CoreV1Interface, params *parameters.Globals, cr *imageregistryv1.Config) *generatorServiceAccount {
	return &generatorServiceAccount{
		lister:    lister,
		client:    client,
		name:      params.Pod.ServiceAccount,
		namespace: params.Deployment.Namespace,
	}
}

func (gsa *generatorServiceAccount) Type() runtime.Object {
	return &corev1.ServiceAccount{}
}

func (gsa *generatorServiceAccount) GetGroup() string {
	return corev1.GroupName
}

func (gsa *generatorServiceAccount) GetResource() string {
	return "serviceaccounts"
}

func (gsa *generatorServiceAccount) GetNamespace() string {
	return gsa.namespace
}

func (gsa *generatorServiceAccount) GetName() string {
	return gsa.name
}

func (gsa *generatorServiceAccount) expected() (runtime.Object, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gsa.GetName(),
			Namespace: gsa.GetNamespace(),
		},
	}

	return sa, nil
}

func (gsa *generatorServiceAccount) Get() (runtime.Object, error) {
	return gsa.lister.Get(gsa.GetName())
}

func (gsa *generatorServiceAccount) Create() (runtime.Object, error) {
	return commonCreate(gsa, func(obj runtime.Object) (runtime.Object, error) {
		return gsa.client.ServiceAccounts(gsa.GetNamespace()).Create(obj.(*corev1.ServiceAccount))
	})
}

func (gsa *generatorServiceAccount) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gsa, o, func(obj runtime.Object) (runtime.Object, error) {
		return gsa.client.ServiceAccounts(gsa.GetNamespace()).Update(obj.(*corev1.ServiceAccount))
	})
}

func (gsa *generatorServiceAccount) Delete(opts *metav1.DeleteOptions) error {
	return gsa.client.ServiceAccounts(gsa.GetNamespace()).Delete(gsa.GetName(), opts)
}

func (g *generatorServiceAccount) Owned() bool {
	return true
}
