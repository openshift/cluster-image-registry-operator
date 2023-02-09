package operator

import (
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configv1informers "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configv1listers "github.com/openshift/client-go/config/listers/config/v1"
	imageregistryv1informers "github.com/openshift/client-go/imageregistry/informers/externalversions/imageregistry/v1"
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
)

type ClusterOperatorStatusController struct {
	relatedObjects []configv1.ObjectReference

	clusterOperatorClient     configv1client.ClusterOperatorsGetter
	clusterOperatorLister     configv1listers.ClusterOperatorLister
	imageRegistryConfigLister imageregistryv1listers.ConfigLister
	imagePrunerLister         imageregistryv1listers.ImagePrunerLister
	deploymentLister          appsv1listers.DeploymentNamespaceLister

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

func NewClusterOperatorStatusController(
	relatedObjects []configv1.ObjectReference,
	configClient configv1client.ConfigV1Interface,
	clusterOperatorInformer configv1informers.ClusterOperatorInformer,
	imageRegistryConfigInformer imageregistryv1informers.ConfigInformer,
	imagePrunerInformer imageregistryv1informers.ImagePrunerInformer,
	deploymentInformer appsv1informers.DeploymentInformer,
) (*ClusterOperatorStatusController, error) {
	c := &ClusterOperatorStatusController{
		relatedObjects:            relatedObjects,
		clusterOperatorClient:     configClient,
		clusterOperatorLister:     clusterOperatorInformer.Lister(),
		imageRegistryConfigLister: imageRegistryConfigInformer.Lister(),
		imagePrunerLister:         imagePrunerInformer.Lister(),
		deploymentLister:          deploymentInformer.Lister().Deployments(defaults.ImageRegistryOperatorNamespace),
		queue:                     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ClusterOperatorStatusController"),
	}

	if _, err := clusterOperatorInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, clusterOperatorInformer.Informer().HasSynced)

	if _, err := imageRegistryConfigInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, imageRegistryConfigInformer.Informer().HasSynced)

	if _, err := imagePrunerInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, imagePrunerInformer.Informer().HasSynced)

	if _, err := deploymentInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, deploymentInformer.Informer().HasSynced)

	return c, nil
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

	klog.V(4).Infof("get event from workqueue")
	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("unable to sync ClusterOperatorStatusController: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.V(4).Infof("event from workqueue successfully processed")
	}
	return true
}

func (c *ClusterOperatorStatusController) sync() error {
	cr, err := c.imageRegistryConfigLister.Get("cluster")
	if err != nil {
		return err
	}
	cr = cr.DeepCopy()

	imagepruner, err := c.imagePrunerLister.Get("cluster")
	if err != nil {
		if !errors.IsNotFound(err) {
			klog.Warningf("unable to get imagepruner: %v", err)
		}
		imagepruner = nil
	}

	mut := resource.NewGeneratorClusterOperator(
		c.deploymentLister,
		c.clusterOperatorLister,
		c.clusterOperatorClient,
		cr,
		imagepruner,
		c.relatedObjects,
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
