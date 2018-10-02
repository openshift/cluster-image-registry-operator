package generate

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func ConfigMap(cr *v1alpha1.ImageRegistry, p *parameters.Globals) (Template, error) {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.ObjectMeta.Name + "-certificates",
			Namespace: p.Deployment.Namespace,
		},
	}
	addOwnerRefToObject(cm, asOwner(cr))
	return Template{
		Object:   cm,
		Strategy: strategy.ConfigMap{},
	}, nil
}
