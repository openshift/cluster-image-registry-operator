package resource

import (
	rbacapi "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"
	rbaclisters "k8s.io/client-go/listers/rbac/v1"
)

var _ Mutator = &generatorPrunerClusterRoleBinding{}

type generatorPrunerClusterRoleBinding struct {
	lister rbaclisters.ClusterRoleBindingLister
	client rbacset.RbacV1Interface
}

func newGeneratorPrunerClusterRoleBinding(lister rbaclisters.ClusterRoleBindingLister, client rbacset.RbacV1Interface) *generatorPrunerClusterRoleBinding {
	return &generatorPrunerClusterRoleBinding{
		lister: lister,
		client: client,
	}
}

func (gcrb *generatorPrunerClusterRoleBinding) Type() runtime.Object {
	return &rbacapi.ClusterRoleBinding{}
}

func (gcrb *generatorPrunerClusterRoleBinding) GetGroup() string {
	return rbacapi.GroupName
}

func (gcrb *generatorPrunerClusterRoleBinding) GetResource() string {
	return "clusterrolebindings"
}

func (gcrb *generatorPrunerClusterRoleBinding) GetNamespace() string {
	return "openshift-image-registry"
}

func (gcrb *generatorPrunerClusterRoleBinding) GetName() string {
	return "openshift-image-registry-pruner"
}

func (gcrb *generatorPrunerClusterRoleBinding) expected() (runtime.Object, error) {
	crb := &rbacapi.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacapi.SchemeGroupVersion.String(),
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: gcrb.GetName(),
		},
		Subjects: []rbacapi.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "pruner",
				Namespace: gcrb.GetNamespace(),
			},
		},
		RoleRef: rbacapi.RoleRef{
			Kind: "ClusterRole",
			Name: "system:image-pruner",
		},
	}

	return crb, nil
}

func (gcrb *generatorPrunerClusterRoleBinding) Get() (runtime.Object, error) {
	return gcrb.lister.Get(gcrb.GetName())
}

func (gcrb *generatorPrunerClusterRoleBinding) Create() (runtime.Object, error) {
	return commonCreate(gcrb, func(obj runtime.Object) (runtime.Object, error) {
		return gcrb.client.ClusterRoleBindings().Create(obj.(*rbacapi.ClusterRoleBinding))
	})
}

func (gcrb *generatorPrunerClusterRoleBinding) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gcrb, o, func(obj runtime.Object) (runtime.Object, error) {
		return gcrb.client.ClusterRoleBindings().Update(obj.(*rbacapi.ClusterRoleBinding))
	})
}

func (gcrb *generatorPrunerClusterRoleBinding) Delete(opts *metav1.DeleteOptions) error {
	return gcrb.client.ClusterRoleBindings().Delete(gcrb.GetName(), opts)
}

func (g *generatorPrunerClusterRoleBinding) Owned() bool {
	return true
}
