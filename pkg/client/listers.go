package client

import (
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	rbaclisters "k8s.io/client-go/listers/rbac/v1"

	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"

	regoplisters "github.com/openshift/cluster-image-registry-operator/pkg/generated/listers/imageregistry/v1alpha1"
)

type Listers struct {
	Deployments         appslisters.DeploymentNamespaceLister
	Services            corelisters.ServiceNamespaceLister
	Secrets             corelisters.SecretNamespaceLister
	ConfigMaps          corelisters.ConfigMapNamespaceLister
	ServiceAccounts     corelisters.ServiceAccountNamespaceLister
	Routes              routelisters.RouteNamespaceLister
	ClusterRoles        rbaclisters.ClusterRoleLister
	ClusterRoleBindings rbaclisters.ClusterRoleBindingLister
	OpenShiftConfig     corelisters.ConfigMapNamespaceLister
	ImageConfigs        configlisters.ImageLister
	ImageRegistry       regoplisters.ImageRegistryLister
}
