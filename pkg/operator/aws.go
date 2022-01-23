package operator

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	imageregistryclient "github.com/openshift/client-go/imageregistry/clientset/versioned"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	routeclient "github.com/openshift/client-go/route/clientset/versioned"
	routeinformers "github.com/openshift/client-go/route/informers/externalversions"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
)

type AWSController struct {
	listers *regopclient.Listers
	clients *regopclient.Clients

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

func NewAWSController(
	kubeClient kubeclient.Interface,
	configClient configclient.Interface,
	imageregistryClient imageregistryclient.Interface,
	routeClient routeclient.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	openshiftConfigKubeInformerFactory kubeinformers.SharedInformerFactory,
	openshiftConfigManagedKubeInformerFactory kubeinformers.SharedInformerFactory,
	configInformerFactory configinformers.SharedInformerFactory,
	regopInformerFactory imageregistryinformers.SharedInformerFactory,
	routeInformerFactory routeinformers.SharedInformerFactory,
) *AWSController {
	c := &AWSController{
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AWSController"),
	}

	c.clients = &regopclient.Clients{
		Core:   kubeClient.CoreV1(),
		Apps:   kubeClient.AppsV1(),
		RBAC:   kubeClient.RbacV1(),
		Kube:   kubeClient,
		Route:  routeClient.RouteV1(),
		Config: configClient.ConfigV1(),
		RegOp:  imageregistryClient,
		Batch:  kubeClient.BatchV1(),
	}

	infraConfig := configInformerFactory.Config().V1().Infrastructures()
	c.listers = &regopclient.Listers{
		Deployments: kubeInformerFactory.Apps().V1().Deployments().
			Lister().Deployments(defaults.ImageRegistryOperatorNamespace),
		Services: kubeInformerFactory.Core().V1().Services().
			Lister().Services(defaults.ImageRegistryOperatorNamespace),
		Secrets: kubeInformerFactory.Core().V1().Secrets().
			Lister().Secrets(defaults.ImageRegistryOperatorNamespace),
		ConfigMaps: kubeInformerFactory.Core().V1().ConfigMaps().
			Lister().ConfigMaps(defaults.ImageRegistryOperatorNamespace),
		ServiceAccounts: kubeInformerFactory.Core().V1().ServiceAccounts().
			Lister().ServiceAccounts(defaults.ImageRegistryOperatorNamespace),
		PodDisruptionBudgets: kubeInformerFactory.Policy().V1().PodDisruptionBudgets().
			Lister().PodDisruptionBudgets(defaults.ImageRegistryOperatorNamespace),
		Routes: routeInformerFactory.Route().V1().Routes().
			Lister().Routes(defaults.ImageRegistryOperatorNamespace),
		ClusterRoles:        kubeInformerFactory.Rbac().V1().ClusterRoles().Lister(),
		ClusterRoleBindings: kubeInformerFactory.Rbac().V1().ClusterRoleBindings().Lister(),
		OpenShiftConfig: openshiftConfigKubeInformerFactory.Core().V1().ConfigMaps().
			Lister().ConfigMaps(defaults.OpenShiftConfigNamespace),
		OpenShiftConfigManaged: openshiftConfigManagedKubeInformerFactory.Core().V1().ConfigMaps().
			Lister().ConfigMaps(defaults.OpenShiftConfigManagedNamespace),
		ProxyConfigs:    configInformerFactory.Config().V1().Proxies().Lister(),
		RegistryConfigs: regopInformerFactory.Imageregistry().V1().Configs().Lister(),
		Infrastructures: infraConfig.Lister(),
	}

	infraConfig.Informer().AddEventHandler(c.eventHandler())
	c.cachesToSync = append(c.cachesToSync, infraConfig.Informer().HasSynced)

	return c
}

func (c *AWSController) eventHandler() cache.ResourceEventHandler {
	const workQueueKey = "aws"
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(workQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(workQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(workQueueKey) },
	}
}

func (c *AWSController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *AWSController) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(obj)

	klog.V(4).Infof("AWSController: got event from workqueue")
	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("AWSController: failed to process event: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.V(4).Infof("AWSController: event from workqueue successfully processed")
	}
	return true
}

func (c *AWSController) Run(ctx context.Context) {
	defer k8sruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting AWSController")
	if !cache.WaitForCacheSync(ctx.Done(), c.cachesToSync...) {
		return
	}

	go wait.Until(c.runWorker, time.Second, ctx.Done())

	klog.Infof("Started AWSController")
	<-ctx.Done()
	klog.Infof("Shutting down AWSController")
}

func (c *AWSController) sync() error {
	cr, err := c.clients.RegOp.ImageregistryV1().Configs().Get(context.Background(),
		defaults.ImageRegistryResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		return err
	}
	// make a copy to avoid changing the cached data
	cr = cr.DeepCopy()

	// if s3 storage config is missing, must be
	// non-AWS platform, so not treating it as error
	if cr.Spec.Storage.S3 == nil {
		return nil
	}

	// Create a driver with the current configuration
	ctx := context.Background()
	driver := s3.NewDriver(ctx, cr.Spec.Storage.S3, c.listers)

	return c.syncTags(driver)
}

func (c *AWSController) syncTags(driver interface{}) error {
	tagset, err := s3.GetStorageTags(driver)
	if err != nil {
		klog.Errorf("syncTags: %v", err)
		return err
	}
	klog.Infof("aws bucket tags: %v", tagset)

	infra, err := c.clients.Config.Infrastructures().Get(
		context.Background(),
		defaults.InfrastructureResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		klog.Errorf("syncTags: failed to fetch Infrastructure: %v", err)
		return err
	}
	klog.Infof("tags provided by the user: %v", infra.Spec.PlatformSpec.AWS.ResourceTags)

	newTagSet := make(map[string]string)
	for _, tags := range infra.Spec.PlatformSpec.AWS.ResourceTags {
		value, ok := tagset[tags.Key]
		if !ok || value != tags.Value {
			klog.Infof("%s tag added/updated with value %s", tags.Key, tags.Value)
			newTagSet[tags.Key] = tags.Value
		}
	}

	if err := s3.PutStorageTags(driver, newTagSet); err != nil {
		klog.Errorf("syncTags: %v", err)
	}

	return nil
}
