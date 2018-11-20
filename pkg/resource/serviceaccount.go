package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	return Template{
		Object:   sa,
		Strategy: strategy.Override{},
	}, nil
}
