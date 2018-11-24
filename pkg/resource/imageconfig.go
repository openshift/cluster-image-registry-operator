package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	configapiv1 "github.com/openshift/api/config/v1"
	configsetv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) makeImageConfig(cr *v1alpha1.ImageRegistry) (Template, error) {
	ic := &configapiv1.Image{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configapiv1.SchemeGroupVersion.String(),
			Kind:       "Image",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: g.params.ImageConfig.Name,
		},
		Status: configapiv1.ImageStatus{
			InternalRegistryHostname: cr.Status.InternalRegistryHostname,
		},
	}

	addOwnerRefToObject(ic, asOwner(cr))

	client, err := configsetv1.NewForConfig(g.kubeconfig)
	if err != nil {
		return Template{}, err
	}

	return Template{
		Object:   ic,
		Strategy: strategy.ImageConfig{},
		Get: func() (runtime.Object, error) {
			return client.Images().Get(ic.Name, metav1.GetOptions{})
		},
		Create: func() error {
			_, err := client.Images().Create(ic)
			return err
		},
		Update: func(o runtime.Object) error {
			n := o.(*configapiv1.Image)
			_, err := client.Images().Update(n)
			return err
		},
		Delete: func(opts *metav1.DeleteOptions) error {
			return client.Images().Delete(ic.Name, opts)
		},
	}, nil
}
