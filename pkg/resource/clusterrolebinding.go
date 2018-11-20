package resource

import (
	rbacapi "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"

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

	client, err := rbacset.NewForConfig(g.kubeconfig)
	if err != nil {
		return Template{}, err
	}

	return Template{
		Object:   crb,
		Strategy: strategy.Override{},
		Get: func() (runtime.Object, error) {
			return client.ClusterRoleBindings().Get(crb.Name, metav1.GetOptions{})
		},
		Create: func() error {
			_, err := client.ClusterRoleBindings().Create(crb)
			return err
		},
		Update: func(o runtime.Object) error {
			n := o.(*rbacapi.ClusterRoleBinding)
			_, err := client.ClusterRoleBindings().Update(n)
			return err
		},
		Delete: func(opts *metav1.DeleteOptions) error {
			return client.ClusterRoleBindings().Delete(crb.Name, opts)
		},
	}, nil
}
