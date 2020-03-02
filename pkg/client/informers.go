package client

import (
	"k8s.io/client-go/tools/cache"
)

type Informers struct {
	ClusterOperators    cache.SharedIndexInformer
	ClusterRoleBindings cache.SharedIndexInformer
	ClusterRoles        cache.SharedIndexInformer
	ConfigMaps          cache.SharedIndexInformer
	CronJobs            cache.SharedIndexInformer
	DaemonSets          cache.SharedIndexInformer
	Deployments         cache.SharedIndexInformer
	ImageConfigs        cache.SharedIndexInformer
	ImagePrunerConfigs  cache.SharedIndexInformer
	Infrastructures     cache.SharedIndexInformer
	Jobs                cache.SharedIndexInformer
	OpenShiftConfig     cache.SharedIndexInformer
	ProxyConfigs        cache.SharedIndexInformer
	RegistryConfigs     cache.SharedIndexInformer
	Routes              cache.SharedIndexInformer
	Secrets             cache.SharedIndexInformer
	ServiceAccounts     cache.SharedIndexInformer
	Services            cache.SharedIndexInformer
}
