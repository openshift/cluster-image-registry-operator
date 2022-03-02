package client

import (
	kappslisters "k8s.io/client-go/listers/apps/v1"
	kbatchlisters "k8s.io/client-go/listers/batch/v1"
	kjoblisters "k8s.io/client-go/listers/batch/v1"
	kcorelisters "k8s.io/client-go/listers/core/v1"
	kpolicylisters "k8s.io/client-go/listers/policy/v1"
	krbaclisters "k8s.io/client-go/listers/rbac/v1"

	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	regoplisters "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"
)

// StorageListers is a set of listers that can be used by storage drivers.
type StorageListers struct {
	Infrastructures        configlisters.InfrastructureLister
	OpenShiftConfig        kcorelisters.ConfigMapNamespaceLister
	OpenShiftConfigManaged kcorelisters.ConfigMapNamespaceLister
	Secrets                kcorelisters.SecretNamespaceLister
}

type Listers struct {
	StorageListers
	Deployments          kappslisters.DeploymentNamespaceLister
	Services             kcorelisters.ServiceNamespaceLister
	ConfigMaps           kcorelisters.ConfigMapNamespaceLister
	ServiceAccounts      kcorelisters.ServiceAccountNamespaceLister
	PodDisruptionBudgets kpolicylisters.PodDisruptionBudgetNamespaceLister
	Routes               routelisters.RouteNamespaceLister
	ClusterRoles         krbaclisters.ClusterRoleLister
	ClusterRoleBindings  krbaclisters.ClusterRoleBindingLister
	RegistryConfigs      regoplisters.ConfigLister
	ProxyConfigs         configlisters.ProxyLister
}

type ImagePrunerControllerListers struct {
	Jobs                kjoblisters.JobNamespaceLister
	CronJobs            kbatchlisters.CronJobNamespaceLister
	ServiceAccounts     kcorelisters.ServiceAccountNamespaceLister
	ClusterRoles        krbaclisters.ClusterRoleLister
	ClusterRoleBindings krbaclisters.ClusterRoleBindingLister
	RegistryConfigs     regoplisters.ConfigLister
	ImagePrunerConfigs  regoplisters.ImagePrunerLister
	ConfigMaps          kcorelisters.ConfigMapNamespaceLister
	ImageConfigs        configlisters.ImageLister
}
