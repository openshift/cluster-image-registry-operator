package fake

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kfake "k8s.io/client-go/kubernetes/fake"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/client-go/tools/cache"

	configv1 "github.com/openshift/api/config/v1"
	regopv1 "github.com/openshift/api/imageregistry/v1"
	routev1 "github.com/openshift/api/route/v1"
	configv1listers "github.com/openshift/client-go/config/listers/config/v1"
	regopv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	routev1listers "github.com/openshift/client-go/route/listers/route/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
)

// FixturesBuilder helps create an in-memory version of client.Listers.
type FixturesBuilder struct {
	deploymentIndexer          cache.Indexer
	servicesIndexer            cache.Indexer
	secretsIndexer             cache.Indexer
	configMapsIndexer          cache.Indexer
	serviceAcctIndexer         cache.Indexer
	routesIndexer              cache.Indexer
	clusterRolesIndexer        cache.Indexer
	clusterRoleBindingsIndexer cache.Indexer
	registryConfigsIndexer     cache.Indexer
	proxyConfigsIndexer        cache.Indexer
	infraIndexer               cache.Indexer
	nodeIndexer                cache.Indexer

	kClientSet []runtime.Object
}

// Fixtures holds fixtures for unit testing, in forms that are easily consumed by k8s
// and OpenShift interfaces.
type Fixtures struct {
	Listers    *client.Listers
	KubeClient *kfake.Clientset
}

// NewFixturesBuilder initializes a new instance of FakeListersFactory
func NewFixturesBuilder() *FixturesBuilder {
	factory := &FixturesBuilder{
		deploymentIndexer:          cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		servicesIndexer:            cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		secretsIndexer:             cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		configMapsIndexer:          cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		serviceAcctIndexer:         cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		routesIndexer:              cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		clusterRolesIndexer:        cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		clusterRoleBindingsIndexer: cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		registryConfigsIndexer:     cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		proxyConfigsIndexer:        cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		infraIndexer:               cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		nodeIndexer:                cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}),
		kClientSet:                 []runtime.Object{},
	}
	return factory
}

// AddNodes adds corev1.Nodes to the lister cache
func (f *FixturesBuilder) AddNodes(objs ...*corev1.Node) *FixturesBuilder {
	for _, v := range objs {
		err := f.nodeIndexer.Add(v)
		if err != nil {
			panic(err)
		}
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddDeployments adds appsv1.Deployments to the lister cache
func (f *FixturesBuilder) AddDeployments(objs ...*appsv1.Deployment) *FixturesBuilder {
	for _, v := range objs {
		err := f.deploymentIndexer.Add(v)
		if err != nil {
			panic(err)
		}
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddNamespaces adds corev1.Namespaces to the fixture
func (f *FixturesBuilder) AddNamespaces(objs ...*corev1.Namespace) *FixturesBuilder {
	for _, v := range objs {
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddServices adds corev1.Services to the lister cache
func (f *FixturesBuilder) AddServices(objs ...*corev1.Service) *FixturesBuilder {
	for _, v := range objs {
		err := f.servicesIndexer.Add(v)
		if err != nil {
			panic(err)
		}
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddSecrets adds corev1.Secrets to the lister cache
func (f *FixturesBuilder) AddSecrets(objs ...*corev1.Secret) *FixturesBuilder {
	for _, v := range objs {
		err := f.secretsIndexer.Add(v)
		if err != nil {
			panic(err)
		}
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddConfigMaps adds corev1.ConfigMaps to the lister cache
func (f *FixturesBuilder) AddConfigMaps(objs ...*corev1.ConfigMap) *FixturesBuilder {
	for _, v := range objs {
		err := f.configMapsIndexer.Add(v)
		if err != nil {
			panic(err)
		}
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddServiceAccounts adds corev1.ServiceAccounts to the lister cache
func (f *FixturesBuilder) AddServiceAccounts(objs ...*corev1.ServiceAccount) *FixturesBuilder {
	for _, v := range objs {
		err := f.serviceAcctIndexer.Add(v)
		if err != nil {
			panic(err)
		}
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddRoutes adds route.openshift.io/v1 Routes to the lister cahce
func (f *FixturesBuilder) AddRoutes(objs ...*routev1.Route) *FixturesBuilder {
	for _, v := range objs {
		err := f.routesIndexer.Add(v)
		if err != nil {
			panic(err)
		}
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddClusterRoles adds rbacv1.ClusterRoles to the lister cache
func (f *FixturesBuilder) AddClusterRoles(objs ...*rbacv1.ClusterRole) *FixturesBuilder {
	for _, v := range objs {
		err := f.clusterRolesIndexer.Add(v)
		if err != nil {
			panic(err)
		}
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddClusterRoleBindings adds rbacv1.ClusterRoleBindings to the lister cache
func (f *FixturesBuilder) AddClusterRoleBindings(objs ...*rbacv1.ClusterRoleBinding) *FixturesBuilder {
	for _, v := range objs {
		err := f.clusterRoleBindingsIndexer.Add(v)
		if err != nil {
			panic(err)
		}
		f.kClientSet = append(f.kClientSet, v)
	}
	return f
}

// AddRegistryOperatorConfig adds imageregistry.operator.openshift.io/v1 Config to the lister cache
func (f *FixturesBuilder) AddRegistryOperatorConfig(config *regopv1.Config) *FixturesBuilder {
	err := f.registryConfigsIndexer.Add(config)
	if err != nil {
		panic(err)
	}
	return f
}

// AddProxyConfig adds cluster-wide config.openshift.io/v1 Proxy to the lister cache
func (f *FixturesBuilder) AddProxyConfig(config *configv1.Proxy) *FixturesBuilder {
	err := f.proxyConfigsIndexer.Add(config)
	if err != nil {
		panic(err)
	}
	return f
}

// AddInfraConfig adds cluster-wide config.openshift.io/v1 Infrastructure to the lister cache
func (f *FixturesBuilder) AddInfraConfig(config *configv1.Infrastructure) *FixturesBuilder {
	err := f.infraIndexer.Add(config)
	if err != nil {
		panic(err)
	}
	return f
}

// Build creates the fixtures from the provided objects.
func (f *FixturesBuilder) Build() *Fixtures {
	fixtures := &Fixtures{
		Listers:    f.BuildListers(),
		KubeClient: kfake.NewSimpleClientset(f.kClientSet...),
	}
	return fixtures
}

// BuildListers creates an in-memory instance of client.Listers
func (f *FixturesBuilder) BuildListers() *client.Listers {
	listers := &client.Listers{
		StorageListers: client.StorageListers{
			Infrastructures:        configv1listers.NewInfrastructureLister(f.infraIndexer),
			OpenShiftConfig:        corev1listers.NewConfigMapLister(f.configMapsIndexer).ConfigMaps("openshift-config"),
			OpenShiftConfigManaged: corev1listers.NewConfigMapLister(f.configMapsIndexer).ConfigMaps("openshift-config-managed"),
			Secrets:                corev1listers.NewSecretLister(f.secretsIndexer).Secrets("openshift-image-registry"),
		},
		Deployments:         appsv1listers.NewDeploymentLister(f.deploymentIndexer).Deployments("openshift-image-registry"),
		Services:            corev1listers.NewServiceLister(f.servicesIndexer).Services("openshift-image-registry"),
		ConfigMaps:          corev1listers.NewConfigMapLister(f.configMapsIndexer).ConfigMaps("openshift-image-registry"),
		ServiceAccounts:     corev1listers.NewServiceAccountLister(f.serviceAcctIndexer).ServiceAccounts("openshift-image-registry"),
		Routes:              routev1listers.NewRouteLister(f.routesIndexer).Routes("openshift-image-registry"),
		ClusterRoles:        rbacv1listers.NewClusterRoleLister(f.clusterRolesIndexer),
		ClusterRoleBindings: rbacv1listers.NewClusterRoleBindingLister(f.clusterRoleBindingsIndexer),
		RegistryConfigs:     regopv1listers.NewConfigLister(f.registryConfigsIndexer),
		ProxyConfigs:        configv1listers.NewProxyLister(f.proxyConfigsIndexer),
	}
	return listers
}
