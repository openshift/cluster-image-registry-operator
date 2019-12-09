package resource

import (
	rbacapi "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"
	rbaclisters "k8s.io/client-go/listers/rbac/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
)

var _ Mutator = &generatorClusterRole{}

type generatorClusterRole struct {
	lister rbaclisters.ClusterRoleLister
	client rbacset.RbacV1Interface
}

func newGeneratorClusterRole(lister rbaclisters.ClusterRoleLister, client rbacset.RbacV1Interface, cr *imageregistryv1.Config) *generatorClusterRole {
	return &generatorClusterRole{
		lister: lister,
		client: client,
	}
}

func (gcr *generatorClusterRole) Type() runtime.Object {
	return &rbacapi.ClusterRole{}
}

func (gcr *generatorClusterRole) GetGroup() string {
	return rbacapi.GroupName
}

func (gcr *generatorClusterRole) GetResource() string {
	return "clusterroles"
}

func (gcr *generatorClusterRole) GetNamespace() string {
	return ""
}

func (gcr *generatorClusterRole) GetName() string {
	return "system:registry"
}

func (gcr *generatorClusterRole) expected() (runtime.Object, error) {
	role := &rbacapi.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gcr.GetName(),
			Namespace: gcr.GetNamespace(),
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
					"imagestreams/layers",
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

	return role, nil
}

func (gcr *generatorClusterRole) Get() (runtime.Object, error) {
	return gcr.lister.Get(gcr.GetName())
}

func (gcr *generatorClusterRole) Create() (runtime.Object, error) {
	return commonCreate(gcr, func(obj runtime.Object) (runtime.Object, error) {
		return gcr.client.ClusterRoles().Create(obj.(*rbacapi.ClusterRole))
	})
}

func (gcr *generatorClusterRole) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gcr, o, func(obj runtime.Object) (runtime.Object, error) {
		return gcr.client.ClusterRoles().Update(obj.(*rbacapi.ClusterRole))
	})
}

func (gcr *generatorClusterRole) Delete(opts *metav1.DeleteOptions) error {
	return gcr.client.ClusterRoles().Delete(gcr.GetName(), opts)
}

func (g *generatorClusterRole) Owned() bool {
	return true
}
