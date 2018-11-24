package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) makeConfigMap(cr *v1alpha1.ImageRegistry) (Template, error) {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.ObjectMeta.Name + "-certificates",
			Namespace: g.params.Deployment.Namespace,
		},
	}

	addOwnerRefToObject(cm, asOwner(cr))

	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return Template{}, err
	}

	return Template{
		Object:   cm,
		Strategy: strategy.ConfigMap{},
		Get: func() (runtime.Object, error) {
			return client.ConfigMaps(cm.Namespace).Get(cm.Name, metav1.GetOptions{})
		},
		Create: func() error {
			_, err := client.ConfigMaps(cm.Namespace).Create(cm)
			return err
		},
		Update: func(o runtime.Object) error {
			n := o.(*corev1.ConfigMap)
			_, err := client.ConfigMaps(cm.Namespace).Update(n)
			return err
		},
		Delete: func(opts *metav1.DeleteOptions) error {
			return client.ConfigMaps(cm.Namespace).Delete(cm.Name, opts)
		},
	}, nil
}
