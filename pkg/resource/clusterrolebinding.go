package resource

import (
	rbacapi "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) makeClusterRoleBinding(cr *v1alpha1.ImageRegistry) (Template, error) {
	crb := &rbacapi.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacapi.SchemeGroupVersion.String(),
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "registry-registry-role",
		},
		Subjects: []rbacapi.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      g.params.Pod.ServiceAccount,
				Namespace: g.params.Deployment.Namespace,
			},
		},
		RoleRef: rbacapi.RoleRef{
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
