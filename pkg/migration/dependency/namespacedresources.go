package dependency

import (
	coreapi "k8s.io/api/core/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

type NamespacedResources interface {
	Secret(name string) (*coreapi.Secret, error)
	ConfigMap(name string) (*coreapi.ConfigMap, error)
}

type namespacedResources struct {
	namespace  string
	kubeconfig *rest.Config
}

func NewNamespacedResources(kubeconfig *rest.Config, namespace string) NamespacedResources {
	return &namespacedResources{
		namespace:  namespace,
		kubeconfig: kubeconfig,
	}
}

func (nr *namespacedResources) Secret(name string) (*coreapi.Secret, error) {
	client, err := coreset.NewForConfig(nr.kubeconfig)
	if err != nil {
		return nil, err
	}

	return client.Secrets(nr.namespace).Get(name, metaapi.GetOptions{})
}

func (nr *namespacedResources) ConfigMap(name string) (*coreapi.ConfigMap, error) {
	client, err := coreset.NewForConfig(nr.kubeconfig)
	if err != nil {
		return nil, err
	}

	return client.ConfigMaps(nr.namespace).Get(name, metaapi.GetOptions{})
}
