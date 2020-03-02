package client

import (
	"fmt"

	kubeinformers "k8s.io/client-go/informers"
	kubeset "k8s.io/client-go/kubernetes"
	appsset "k8s.io/client-go/kubernetes/typed/apps/v1"
	batchset "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	configset "github.com/openshift/client-go/config/clientset/versioned"
	configsetv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	regopset "github.com/openshift/client-go/imageregistry/clientset/versioned"
	regopinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	routeset "github.com/openshift/client-go/route/clientset/versioned"
	routesetv1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	routeinformers "github.com/openshift/client-go/route/informers/externalversions"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

type Generator struct {
	Kubeconfig *rest.Config
	Params     *parameters.Globals
	Clients    *Clients
	Listers    *Listers
	Informers  *Informers
}

func NewGenerator(kubeconfig *restclient.Config, params *parameters.Globals, stopCh <-chan struct{}) (*Generator, error) {
	var err error

	g := Generator{
		Kubeconfig: kubeconfig,
		Params:     params,
		Clients:    &Clients{},
		Listers:    &Listers{},
		Informers:  &Informers{},
	}

	g.Clients.Core, err = coreset.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	g.Clients.Apps, err = appsset.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	g.Clients.RBAC, err = rbacset.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	g.Clients.Kube, err = kubeset.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	g.Clients.Route, err = routesetv1.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	g.Clients.Config, err = configsetv1.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	g.Clients.RegOp, err = regopset.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	g.Clients.Batch, err = batchset.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	routeClient, err := routeset.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	configClient, err := configset.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	g.Clients.Batch, err = batchset.NewForConfig(g.Kubeconfig)
	if err != nil {
		return nil, err
	}

	configInformerFactory := configinformers.NewSharedInformerFactory(configClient, defaults.ResyncDuration)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(g.Clients.Kube, defaults.ResyncDuration, kubeinformers.WithNamespace(g.Params.Deployment.Namespace))
	openshiftConfigKubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(g.Clients.Kube, defaults.ResyncDuration, kubeinformers.WithNamespace(defaults.OpenshiftConfigNamespace))
	regopInformerFactory := regopinformers.NewSharedInformerFactory(g.Clients.RegOp, defaults.ResyncDuration)
	routeInformerFactory := routeinformers.NewSharedInformerFactoryWithOptions(routeClient, defaults.ResyncDuration, routeinformers.WithNamespace(g.Params.Deployment.Namespace))

	var informersHaveSynced []cache.InformerSynced

	clusterOperators := configInformerFactory.Config().V1().ClusterOperators()
	g.Listers.ClusterOperators = clusterOperators.Lister()
	g.Informers.ClusterOperators = clusterOperators.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.ClusterOperators.HasSynced)

	clusterRoleBindings := kubeInformerFactory.Rbac().V1().ClusterRoleBindings()
	g.Listers.ClusterRoleBindings = clusterRoleBindings.Lister()
	g.Informers.ClusterRoleBindings = clusterRoleBindings.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.ClusterRoleBindings.HasSynced)

	clusterRoles := kubeInformerFactory.Rbac().V1().ClusterRoles()
	g.Listers.ClusterRoles = clusterRoles.Lister()
	g.Informers.ClusterRoles = clusterRoles.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.ClusterRoles.HasSynced)

	configMaps := kubeInformerFactory.Core().V1().ConfigMaps()
	g.Listers.ConfigMaps = configMaps.Lister().ConfigMaps(g.Params.Deployment.Namespace)
	g.Informers.ConfigMaps = configMaps.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.ConfigMaps.HasSynced)

	configs := regopInformerFactory.Imageregistry().V1().Configs()
	g.Listers.RegistryConfigs = configs.Lister()
	g.Informers.RegistryConfigs = configs.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.RegistryConfigs.HasSynced)

	cronjobs := kubeInformerFactory.Batch().V1beta1().CronJobs()
	g.Listers.CronJobs = cronjobs.Lister().CronJobs(defaults.ImageRegistryOperatorNamespace)
	g.Informers.CronJobs = cronjobs.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.CronJobs.HasSynced)

	daemonsets := kubeInformerFactory.Apps().V1().DaemonSets()
	g.Listers.DaemonSets = daemonsets.Lister().DaemonSets(g.Params.Deployment.Namespace)
	g.Informers.DaemonSets = daemonsets.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.DaemonSets.HasSynced)

	deployments := kubeInformerFactory.Apps().V1().Deployments()
	g.Listers.Deployments = deployments.Lister().Deployments(g.Params.Deployment.Namespace)
	g.Informers.Deployments = deployments.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.Deployments.HasSynced)

	imagePruners := regopInformerFactory.Imageregistry().V1().ImagePruners()
	g.Listers.ImagePrunerConfigs = imagePruners.Lister()
	g.Informers.ImagePrunerConfigs = imagePruners.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.ImagePrunerConfigs.HasSynced)

	images := configInformerFactory.Config().V1().Images()
	g.Listers.ImageConfigs = images.Lister()
	g.Informers.ImageConfigs = images.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.ImageConfigs.HasSynced)

	infrastructures := configInformerFactory.Config().V1().Infrastructures()
	g.Listers.Infrastructures = infrastructures.Lister()
	g.Informers.Infrastructures = infrastructures.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.Infrastructures.HasSynced)

	jobs := kubeInformerFactory.Batch().V1().Jobs()
	g.Listers.Jobs = jobs.Lister().Jobs(defaults.ImageRegistryOperatorNamespace)
	g.Informers.Jobs = jobs.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.Jobs.HasSynced)

	openshiftConfigMaps := openshiftConfigKubeInformerFactory.Core().V1().ConfigMaps()
	g.Listers.OpenShiftConfig = openshiftConfigMaps.Lister().ConfigMaps(defaults.OpenshiftConfigNamespace)
	g.Informers.OpenShiftConfig = openshiftConfigMaps.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.OpenShiftConfig.HasSynced)

	proxies := configInformerFactory.Config().V1().Proxies()
	g.Listers.ProxyConfigs = proxies.Lister()
	g.Informers.ProxyConfigs = proxies.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.ProxyConfigs.HasSynced)

	routes := routeInformerFactory.Route().V1().Routes()
	g.Listers.Routes = routes.Lister().Routes(g.Params.Deployment.Namespace)
	g.Informers.Routes = routes.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.Routes.HasSynced)

	secrets := kubeInformerFactory.Core().V1().Secrets()
	g.Listers.Secrets = secrets.Lister().Secrets(g.Params.Deployment.Namespace)
	g.Informers.Secrets = secrets.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.Secrets.HasSynced)

	serviceAccounts := kubeInformerFactory.Core().V1().ServiceAccounts()
	g.Listers.ServiceAccounts = serviceAccounts.Lister().ServiceAccounts(g.Params.Deployment.Namespace)
	g.Informers.ServiceAccounts = serviceAccounts.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.ServiceAccounts.HasSynced)

	services := kubeInformerFactory.Core().V1().Services()
	g.Listers.Services = services.Lister().Services(g.Params.Deployment.Namespace)
	g.Informers.Services = services.Informer()
	informersHaveSynced = append(informersHaveSynced, g.Informers.Services.HasSynced)

	configInformerFactory.Start(stopCh)
	kubeInformerFactory.Start(stopCh)
	openshiftConfigKubeInformerFactory.Start(stopCh)
	regopInformerFactory.Start(stopCh)
	routeInformerFactory.Start(stopCh)

	klog.Info("waiting for informer caches to sync")
	for _, synced := range informersHaveSynced {
		if ok := cache.WaitForCacheSync(stopCh, synced); !ok {
			return &g, fmt.Errorf("failed to wait for caches to sync")
		}
	}

	return &g, nil
}
