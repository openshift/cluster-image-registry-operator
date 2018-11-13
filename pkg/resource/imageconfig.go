package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configapiv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func makeImageConfig(cr *v1alpha1.ImageRegistry, p *parameters.Globals) (Template, error) {
	ic := &configapiv1.Image{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configapiv1.SchemeGroupVersion.String(),
			Kind:       "Image",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: p.ImageConfig.Name,
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
