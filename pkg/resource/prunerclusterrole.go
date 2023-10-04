package resource

import (
	"context"

	rbacapi "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"
	rbaclisters "k8s.io/client-go/listers/rbac/v1"
)

var _ Mutator = &generatorPrunerClusterRole{}

type generatorPrunerClusterRole struct {
	lister rbaclisters.ClusterRoleLister
	client rbacset.RbacV1Interface
}

func newGeneratorPrunerClusterRole(lister rbaclisters.ClusterRoleLister, client rbacset.RbacV1Interface) *generatorPrunerClusterRole {
	return &generatorPrunerClusterRole{
		lister: lister,
		client: client,
	}
}

func (gcr *generatorPrunerClusterRole) Type() runtime.Object {
	return &rbacapi.ClusterRole{}
}

func (gcr *generatorPrunerClusterRole) GetNamespace() string {
	return ""
}

func (gcr *generatorPrunerClusterRole) GetName() string {
	return "system:image-pruner"
}

func (gcr *generatorPrunerClusterRole) expected() (runtime.Object, error) {
	role := &rbacapi.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gcr.GetName(),
			Namespace: gcr.GetNamespace(),
		},
		Rules: []rbacapi.PolicyRule{
			{
				Verbs:     []string{"get", "list"},
				APIGroups: []string{""},
				Resources: []string{
					"pods",
					"replicationcontrollers",
				},
			},
			{
				Verbs:     []string{"list"},
				APIGroups: []string{""},
				Resources: []string{
					"limitranges",
				},
			},
			{
				Verbs:     []string{"get", "list"},
				APIGroups: []string{"build.openshift.io", ""},
				Resources: []string{
					"buildconfigs",
					"builds",
				},
			},
			{
				Verbs:     []string{"get", "list"},
				APIGroups: []string{"apps.openshift.io", ""},
				Resources: []string{
					"deploymentconfigs",
				},
			},
			{
				Verbs:     []string{"get", "list"},
				APIGroups: []string{"batch"},
				Resources: []string{
					"jobs",
					"cronjobs",
				},
			},
			{
				Verbs:     []string{"get", "list"},
				APIGroups: []string{"apps"},
				Resources: []string{
					"daemonsets",
					"deployments",
					"replicasets",
					"statefulsets",
				},
			},
			{
				Verbs:     []string{"delete"},
				APIGroups: []string{"image.openshift.io", ""},
				Resources: []string{
					"images",
				},
			},
			{
				Verbs:     []string{"get", "list", "watch"},
				APIGroups: []string{"image.openshift.io", ""},
				Resources: []string{
					"images",
					"imagestreams",
				},
			},
			{
				Verbs:     []string{"update"},
				APIGroups: []string{"image.openshift.io", ""},
				Resources: []string{
					"imagestreams/status",
				},
			},
		},
	}

	return role, nil
}

func (gcr *generatorPrunerClusterRole) Get() (runtime.Object, error) {
	return gcr.lister.Get(gcr.GetName())
}

func (gcr *generatorPrunerClusterRole) Create() (runtime.Object, error) {
	return commonCreate(gcr, func(obj runtime.Object) (runtime.Object, error) {
		return gcr.client.ClusterRoles().Create(
			context.TODO(), obj.(*rbacapi.ClusterRole), metav1.CreateOptions{},
		)
	})
}

func (gcr *generatorPrunerClusterRole) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gcr, o, func(obj runtime.Object) (runtime.Object, error) {
		return gcr.client.ClusterRoles().Update(
			context.TODO(), obj.(*rbacapi.ClusterRole), metav1.UpdateOptions{},
		)
	})
}

func (gcr *generatorPrunerClusterRole) Delete(opts metav1.DeleteOptions) error {
	return gcr.client.ClusterRoles().Delete(
		context.TODO(), gcr.GetName(), opts,
	)
}

func (g *generatorPrunerClusterRole) Owned() bool {
	return true
}
