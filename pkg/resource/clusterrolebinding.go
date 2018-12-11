package resource

import (
	rbacapi "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"
	rbaclisters "k8s.io/client-go/listers/rbac/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var _ Mutator = &generatorClusterRoleBinding{}

type generatorClusterRoleBinding struct {
	lister      rbaclisters.ClusterRoleBindingLister
	client      rbacset.RbacV1Interface
	saName      string
	saNamespace string
	owner       metav1.OwnerReference
}

func newGeneratorClusterRoleBinding(lister rbaclisters.ClusterRoleBindingLister, client rbacset.RbacV1Interface, params *parameters.Globals, cr *regopapi.ImageRegistry) *generatorClusterRoleBinding {
	return &generatorClusterRoleBinding{
		lister:      lister,
		client:      client,
		saName:      params.Pod.ServiceAccount,
		saNamespace: params.Deployment.Namespace,
		owner:       asOwner(cr),
	}
}

func (gcrb *generatorClusterRoleBinding) Type() runtime.Object {
	return &rbacapi.ClusterRoleBinding{}
}

func (gcrb *generatorClusterRoleBinding) GetNamespace() string {
	return ""
}

func (gcrb *generatorClusterRoleBinding) GetName() string {
	return "registry-registry-role"
}

func (gcrb *generatorClusterRoleBinding) expected() (runtime.Object, error) {
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
				Name:      gcrb.saName,
				Namespace: gcrb.saNamespace,
			},
		},
		RoleRef: rbacapi.RoleRef{
			Kind: "ClusterRole",
			Name: "system:registry",
		},
	}

	addOwnerRefToObject(crb, gcrb.owner)

	return crb, nil
}

func (gcrb *generatorClusterRoleBinding) Get() (runtime.Object, error) {
	return gcrb.lister.Get(gcrb.GetName())
}

func (gcrb *generatorClusterRoleBinding) Create() error {
	return commonCreate(gcrb, func(obj runtime.Object) (runtime.Object, error) {
		return gcrb.client.ClusterRoleBindings().Create(obj.(*rbacapi.ClusterRoleBinding))
	})
}

func (gcrb *generatorClusterRoleBinding) Update(o runtime.Object) (bool, error) {
	return commonUpdate(gcrb, o, func(obj runtime.Object) (runtime.Object, error) {
		return gcrb.client.ClusterRoleBindings().Update(obj.(*rbacapi.ClusterRoleBinding))
	})
}

func (gcrb *generatorClusterRoleBinding) Delete(opts *metav1.DeleteOptions) error {
	return gcrb.client.ClusterRoleBindings().Delete(gcrb.GetName(), opts)
}
