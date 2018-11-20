package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) makeServiceAccount(cr *v1alpha1.ImageRegistry) (Template, error) {
	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.params.Pod.ServiceAccount,
			Namespace: g.params.Deployment.Namespace,
		},
	}
	addOwnerRefToObject(sa, asOwner(cr))

	client, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return Template{}, err
	}

	return Template{
		Object:   sa,
		Strategy: strategy.Override{},
		Get: func() (runtime.Object, error) {
			return client.ServiceAccounts(sa.Namespace).Get(sa.Name, metav1.GetOptions{})
		},
		Create: func() error {
			_, err := client.ServiceAccounts(sa.Namespace).Create(sa)
			return err
		},
		Update: func(o runtime.Object) error {
			n := o.(*corev1.ServiceAccount)
			_, err := client.ServiceAccounts(sa.Namespace).Update(n)
			return err
		},
		Delete: func(opts *metav1.DeleteOptions) error {
			return client.ServiceAccounts(sa.Namespace).Delete(sa.Name, opts)
		},
	}, nil
}
