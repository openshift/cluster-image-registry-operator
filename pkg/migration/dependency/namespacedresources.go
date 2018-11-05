package dependency

import (
	"github.com/operator-framework/operator-sdk/pkg/sdk"

	coreapi "k8s.io/api/core/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NamespacedResources interface {
	Secret(name string) (*coreapi.Secret, error)
	ConfigMap(name string) (*coreapi.ConfigMap, error)
}

type namespacedResources struct {
	namespace string
}

func NewNamespacedResources(namespace string) NamespacedResources {
	return &namespacedResources{
		namespace: namespace,
	}
}

func (nr *namespacedResources) Secret(name string) (*coreapi.Secret, error) {
	secret := &coreapi.Secret{
		TypeMeta: metaapi.TypeMeta{
			APIVersion: coreapi.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metaapi.ObjectMeta{
			Name:      name,
			Namespace: nr.namespace,
		},
	}
	return secret, sdk.Get(secret)
}

func (nr *namespacedResources) ConfigMap(name string) (*coreapi.ConfigMap, error) {
	configMap := &coreapi.ConfigMap{
		TypeMeta: metaapi.TypeMeta{
			APIVersion: coreapi.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metaapi.ObjectMeta{
			Name:      name,
			Namespace: nr.namespace,
		},
	}
	return configMap, sdk.Get(configMap)
}
