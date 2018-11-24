package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) getSecret(name, namespace string) (*corev1.Secret, error) {
	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}
	return client.Secrets(namespace).Get(name, metav1.GetOptions{})
}

func (g *Generator) makeSecret(cr *v1alpha1.ImageRegistry) (Template, error) {
	s := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.ObjectMeta.Name + "-private-configuration",
			Namespace: g.params.Deployment.Namespace,
		},
	}

	addOwnerRefToObject(s, asOwner(cr))

	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return Template{}, err
	}

	return Template{
		Object:   s,
		Strategy: strategy.Secret{},
		Get: func() (runtime.Object, error) {
			return client.Secrets(s.Namespace).Get(s.Name, metav1.GetOptions{})
		},
		Create: func() error {
			_, err := client.Secrets(s.Namespace).Create(s)
			return err
		},
		Update: func(o runtime.Object) error {
			n := o.(*corev1.Secret)
			_, err := client.Secrets(s.Namespace).Update(n)
			return err
		},
		Delete: func(opts *metav1.DeleteOptions) error {
			return client.Secrets(s.Namespace).Delete(s.Name, opts)
		},
	}, nil
}
