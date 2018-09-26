package generate

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func getSecret(name, namespace string) (*corev1.Secret, error) {
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

func Secret(cr *v1alpha1.ImageRegistry, p *parameters.Globals) Template {
	s := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry-private-configuration",
			Namespace: p.Deployment.Namespace,
		},
	}
	addOwnerRefToObject(s, asOwner(cr))
	return Template{
		Object:   s,
		Strategy: strategy.Secret{},
	}
}
