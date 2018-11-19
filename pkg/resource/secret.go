package resource

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) getSecret(name, namespace string) (*corev1.Secret, error) {
	o := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	err := sdk.Get(o)
	if err != nil {
		return nil, err
	}

	return o, nil
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
	return Template{
		Object:   s,
		Strategy: strategy.Secret{},
	}, nil
}
