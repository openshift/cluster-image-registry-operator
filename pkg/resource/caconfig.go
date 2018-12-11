package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var _ Mutator = &generatorCAConfig{}

type generatorCAConfig struct {
	lister                corelisters.ConfigMapNamespaceLister
	openshiftConfigLister corelisters.ConfigMapNamespaceLister
	client                coreset.CoreV1Interface
	name                  string
	namespace             string
	caConfigName          string
	owner                 metav1.OwnerReference
}

func newGeneratorCAConfig(lister corelisters.ConfigMapNamespaceLister, openshiftConfigLister corelisters.ConfigMapNamespaceLister, client coreset.CoreV1Interface, params *parameters.Globals, cr *regopapi.ImageRegistry) *generatorCAConfig {
	return &generatorCAConfig{
		lister:       lister,
		client:       client,
		name:         params.CAConfig.Name,
		namespace:    params.Deployment.Namespace,
		caConfigName: cr.Spec.CAConfigName,
		owner:        asOwner(cr),
	}
}

func (gcac *generatorCAConfig) Type() runtime.Object {
	return &corev1.ConfigMap{}
}

func (gcac *generatorCAConfig) GetNamespace() string {
	return gcac.namespace
}

func (gcac *generatorCAConfig) GetName() string {
	return gcac.name
}

func (gcac *generatorCAConfig) expected() (runtime.Object, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gcac.GetName(),
			Namespace: gcac.GetNamespace(),
		},
	}

	if gcac.caConfigName != "" {
		upstreamConfig, err := gcac.openshiftConfigLister.Get(gcac.caConfigName)
		if err != nil {
			return nil, err
		}

		for k, v := range upstreamConfig.Data {
			cm.Data[k] = v
		}
		for k, v := range upstreamConfig.BinaryData {
			cm.BinaryData[k] = v
		}
	}

	addOwnerRefToObject(cm, gcac.owner)

	return cm, nil
}

func (gcac *generatorCAConfig) Get() (runtime.Object, error) {
	return gcac.lister.Get(gcac.GetName())
}

func (gcac *generatorCAConfig) Create() error {
	return commonCreate(gcac, func(obj runtime.Object) (runtime.Object, error) {
		return gcac.client.ConfigMaps(gcac.GetNamespace()).Create(obj.(*corev1.ConfigMap))
	})
}

func (gcac *generatorCAConfig) Update(o runtime.Object) (bool, error) {
	return commonUpdate(gcac, o, func(obj runtime.Object) (runtime.Object, error) {
		return gcac.client.ConfigMaps(gcac.GetNamespace()).Update(obj.(*corev1.ConfigMap))
	})
}

func (gcac *generatorCAConfig) Delete(opts *metav1.DeleteOptions) error {
	return gcac.client.ConfigMaps(gcac.GetNamespace()).Delete(gcac.GetName(), opts)
}
