package operator

import (
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configv1informers "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configv1listers "github.com/openshift/client-go/config/listers/config/v1"
	imageregistryv1informers "github.com/openshift/client-go/imageregistry/informers/externalversions/imageregistry/v1"
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

type ClusterOperatorStatusController struct {
	clusterOperatorClient     configv1client.ClusterOperatorsGetter
	clusterOperatorLister     configv1listers.ClusterOperatorLister
	imageRegistryConfigLister imageregistryv1listers.ConfigLister
	deploymentLister          appsv1listers.DeploymentNamespaceLister

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface

	// These fields should be used only for NewGenerator
	kubeconfig *rest.Config
	clients    *client.Clients
	listers    *client.Listers
}

func NewClusterOperatorStatusController(
	configClient configv1client.ConfigV1Interface,
	clusterOperatorInformer configv1informers.ClusterOperatorInformer,
	imageRegistryConfigInformer imageregistryv1informers.ConfigInformer,
	deploymentInformer appsv1informers.DeploymentInformer,
	kubeconfig *rest.Config, clients *client.Clients, listers *client.Listers,
) *ClusterOperatorStatusController {
	c := &ClusterOperatorStatusController{
		clusterOperatorClient:     configClient,
		clusterOperatorLister:     clusterOperatorInformer.Lister(),
		imageRegistryConfigLister: imageRegistryConfigInformer.Lister(),
		deploymentLister:          deploymentInformer.Lister().Deployments(defaults.ImageRegistryOperatorNamespace),
		queue:                     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ClusterOperatorStatusController"),
		kubeconfig:                kubeconfig,
		clients:                   clients,
		listers:                   listers,
	}

	clusterOperatorInformer.Informer().AddEventHandler(c.eventHandler())
	imageRegistryConfigInformer.Informer().AddEventHandler(c.eventHandler())
	deploymentInformer.Informer().AddEventHandler(c.eventHandler())

	c.cachesToSync = append(c.cachesToSync, clusterOperatorInformer.Informer().HasSynced)
	c.cachesToSync = append(c.cachesToSync, imageRegistryConfigInformer.Informer().HasSynced)
	c.cachesToSync = append(c.cachesToSync, deploymentInformer.Informer().HasSynced)

	return c
}

func (c *ClusterOperatorStatusController) eventHandler() cache.ResourceEventHandler {
	const workQueueKey = "instance"
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(workQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(workQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(workQueueKey) },
	}
}

func (c *ClusterOperatorStatusController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *ClusterOperatorStatusController) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(obj)

	klog.V(1).Infof("get event from workqueue")
	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("unable to sync ClusterOperatorStatusController: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.Infof("event from workqueue successfully processed")
	}
	return true
}

func (c *ClusterOperatorStatusController) sync() error {
	cr, err := c.imageRegistryConfigLister.Get("cluster")
	if err != nil {
		return err
	}
	cr = cr.DeepCopy()

	gen := resource.NewGenerator(c.kubeconfig, c.clients, c.listers, Parameters(defaults.ImageRegistryOperatorNamespace))
	resources, err := gen.List(cr)
	if err != nil && err != storage.ErrStorageNotConfigured {
		return err
	}

	mut := resource.NewGeneratorClusterOperator(
		c.deploymentLister,
		c.clusterOperatorLister,
		c.clusterOperatorClient,
		cr,
		resources,
	)

	return resource.ApplyMutator(mut)
}

func (c *ClusterOperatorStatusController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting ClusterOperatorStatusController")
	if !cache.WaitForCacheSync(stopCh, c.cachesToSync...) {
		return
	}

	go wait.Until(c.runWorker, time.Second, stopCh)

	klog.Infof("Started ClusterOperatorStatusController")
	<-stopCh
	klog.Infof("Shutting down ClusterOperatorStatusController")
}
