package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	return Template{
		Object:   cm,
		Strategy: strategy.ConfigMap{},
	}, nil
}
