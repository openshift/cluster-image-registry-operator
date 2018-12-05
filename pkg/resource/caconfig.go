package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) makeCAConfig(cr *regopapi.ImageRegistry) (Template, error) {
	conf := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: g.params.CAConfig.Name,
		},
	}

	addOwnerRefToObject(conf, asOwner(cr))

	upstreamConfig, err := g.listers.OpenShiftConfig.Get(cr.Spec.CAConfigName)
	if err != nil {
		return Template{}, err
	}

	for k, v := range upstreamConfig.Data {
		conf.Data[k] = v
	}
	for k, v := range upstreamConfig.BinaryData {
		conf.BinaryData[k] = v
	}

	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return Template{}, err
	}

	return Template{
		Object:   conf,
		Strategy: strategy.ConfigMap{},
		Get: func() (runtime.Object, error) {
			return client.ConfigMaps(g.params.Deployment.Namespace).Get(conf.Name, metav1.GetOptions{})
		},
		Create: func() error {
			_, err := client.ConfigMaps(g.params.Deployment.Namespace).Create(conf)
			return err
		},
		Update: func(o runtime.Object) error {
			n := o.(*corev1.ConfigMap)
			_, err := client.ConfigMaps(g.params.Deployment.Namespace).Update(n)
			return err
		},
		Delete: func(opts *metav1.DeleteOptions) error {
			return client.ConfigMaps(g.params.Deployment.Namespace).Delete(conf.Name, opts)
		},
	}, nil
}
