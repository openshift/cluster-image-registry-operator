package generate

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authapi "github.com/openshift/api/authorization/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func ClusterRoleBinding(cr *v1alpha1.ImageRegistry, p *parameters.Globals) (Template, error) {
	crb := &authapi.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: authapi.SchemeGroupVersion.String(),
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "registry-registry-role",
		},
		Subjects: []corev1.ObjectReference{
			{
				Kind:      "ServiceAccount",
				Name:      p.Pod.ServiceAccount,
				Namespace: p.Deployment.Namespace,
			},
		},
		RoleRef: corev1.ObjectReference{
			Kind: "ClusterRole",
			Name: "system:registry",
		},
	}
	addOwnerRefToObject(crb, asOwner(cr))
	return Template{
		Object:   crb,
		Strategy: strategy.Override{},
	}, nil
}
