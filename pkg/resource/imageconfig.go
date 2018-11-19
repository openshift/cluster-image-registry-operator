package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configapiv1 "github.com/openshift/api/config/v1"

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
	return Template{
		Object:   ic,
		Strategy: strategy.ImageConfig{},
	}, nil
}
