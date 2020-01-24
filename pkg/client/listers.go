package client

import (
	kappslisters "k8s.io/client-go/listers/apps/v1"
	kjoblisters "k8s.io/client-go/listers/batch/v1"
	kbatchlisters "k8s.io/client-go/listers/batch/v1beta1"
	kcorelisters "k8s.io/client-go/listers/core/v1"
	krbaclisters "k8s.io/client-go/listers/rbac/v1"

	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	regoplisters "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"
)

type Listers struct {
	Jobs                kjoblisters.JobNamespaceLister
	CronJobs            kbatchlisters.CronJobNamespaceLister
	Deployments         kappslisters.DeploymentNamespaceLister
	DaemonSets          kappslisters.DaemonSetNamespaceLister
	Services            kcorelisters.ServiceNamespaceLister
	Secrets             kcorelisters.SecretNamespaceLister
	ConfigMaps          kcorelisters.ConfigMapNamespaceLister
	ServiceAccounts     kcorelisters.ServiceAccountNamespaceLister
	Routes              routelisters.RouteNamespaceLister
	ClusterRoles        krbaclisters.ClusterRoleLister
	ClusterRoleBindings krbaclisters.ClusterRoleBindingLister
	OpenShiftConfig     kcorelisters.ConfigMapNamespaceLister
	ImageConfigs        configlisters.ImageLister
	ClusterOperators    configlisters.ClusterOperatorLister
	RegistryConfigs     regoplisters.ConfigLister
	ImagePrunerConfigs  regoplisters.ImagePrunerLister
	InstallerConfigMaps kcorelisters.ConfigMapNamespaceLister
	ProxyConfigs        configlisters.ProxyLister
	Infrastructures     configlisters.InfrastructureLister
}
