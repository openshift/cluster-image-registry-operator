package resource

import (
	rbacapi "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) makeClusterRole(cr *v1alpha1.ImageRegistry) (Template, error) {
	role := &rbacapi.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacapi.SchemeGroupVersion.String(),
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "system:registry",
		},
		Rules: []rbacapi.PolicyRule{
			{
				Verbs:     []string{"list"},
				APIGroups: []string{""},
				Resources: []string{
					"limitranges",
					"resourcequotas",
				},
			},
			{
				Verbs:     []string{"get"},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"imagestreamimages",
					/* "imagestreams/layers", */
					"imagestreams/secrets",
				},
			},
			{
				Verbs:     []string{ /* "list", */ "get", "update"},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"imagestreams",
				},
			},
			{
				Verbs:     []string{ /* "get", */ "delete"},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"imagestreamtags",
				},
			},
			{
				Verbs:     []string{"get", "update" /*, "delete" */},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"images",
				},
			},
			{
				Verbs:     []string{"create"},
				APIGroups: []string{ /* "", */ "image.openshift.io"},
				Resources: []string{
					"imagestreammappings",
				},
			},
		},
	}

	addOwnerRefToObject(role, asOwner(cr))

	client, err := rbacset.NewForConfig(g.kubeconfig)
	if err != nil {
		return Template{}, err
	}

	return Template{
		Object:   role,
		Strategy: strategy.Override{},
		Get: func() (runtime.Object, error) {
			return client.ClusterRoles().Get(role.Name, metav1.GetOptions{})
		},
		Create: func() error {
			_, err := client.ClusterRoles().Create(role)
			return err
		},
		Update: func(o runtime.Object) error {
			n := o.(*rbacapi.ClusterRole)
			_, err := client.ClusterRoles().Update(n)
			return err
		},
		Delete: func(opts *metav1.DeleteOptions) error {
			return client.ClusterRoles().Delete(role.Name, opts)
		},
	}, nil
}
