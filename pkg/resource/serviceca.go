package resource

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

var _ Mutator = &generatorServiceCA{}

type generatorServiceCA struct {
	lister    corelisters.ConfigMapNamespaceLister
	client    coreset.CoreV1Interface
	name      string
	namespace string
}

func newGeneratorServiceCA(lister corelisters.ConfigMapNamespaceLister, client coreset.CoreV1Interface) *generatorServiceCA {
	return &generatorServiceCA{
		lister:    lister,
		client:    client,
		name:      defaults.ServiceCAName,
		namespace: defaults.ImageRegistryOperatorNamespace,
	}
}

func (g *generatorServiceCA) Type() runtime.Object {
	return &corev1.ConfigMap{}
}

func (g *generatorServiceCA) GetNamespace() string {
	return g.namespace
}

func (g *generatorServiceCA) GetName() string {
	return g.name
}

func (g *generatorServiceCA) expected() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        g.GetName(),
			Namespace:   g.GetNamespace(),
			Annotations: map[string]string{"service.beta.openshift.io/inject-cabundle": "true"},
		},
		Data:       map[string]string{},
		BinaryData: map[string][]byte{},
	}
}

func (g *generatorServiceCA) Get() (runtime.Object, error) {
	return g.lister.Get(g.GetName())
}

func (g *generatorServiceCA) Create() (runtime.Object, error) {
	o := g.expected()
	cm, err := g.client.ConfigMaps(g.GetNamespace()).Create(
		context.TODO(), o, metav1.CreateOptions{},
	)
	return cm, err
}

func (g *generatorServiceCA) Update(obj runtime.Object) (runtime.Object, bool, error) {
	// We can't use commonUpdate here, because we expect Data to be managed by another operator.
	o := obj.(*corev1.ConfigMap)
	n := g.expected()
	updated := strategy.Metadata(&o.ObjectMeta, &n.ObjectMeta)
	if !updated {
		return o, updated, nil
	}
	u, err := g.client.ConfigMaps(g.GetNamespace()).Update(
		context.TODO(), o, metav1.UpdateOptions{},
	)
	return u, true, err
}

func (g *generatorServiceCA) Delete(opts metav1.DeleteOptions) error {
	return g.client.ConfigMaps(g.GetNamespace()).Delete(
		context.TODO(), g.GetName(), opts,
	)
}

func (g *generatorServiceCA) Owned() bool {
	return true
}
