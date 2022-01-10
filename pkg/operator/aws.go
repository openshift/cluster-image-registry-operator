package operator

import (
	"context"
	"time"

	k8sruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	configv1informers "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configv1listers "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

type AWSController struct {
	operatorClient    v1helpers.OperatorClient
	infraConfigLister configv1listers.InfrastructureLister

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

func NewAWSController(
	operatorClient v1helpers.OperatorClient,
	infraConfigInformer configv1informers.InfrastructureInformer,
) *AWSController {
	c := &AWSController{
		operatorClient:    operatorClient,
		infraConfigLister: infraConfigInformer.Lister(),
		queue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AWSController"),
	}

	infraConfigInformer.Informer().AddEventHandler(c.eventHandler())
	c.cachesToSync = append(c.cachesToSync, infraConfigInformer.Informer().HasSynced)

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
	if err := c.handleEvent(); err != nil {
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

func (c *AWSController) handleEvent() error {
	return c.syncTags()
}

func (c *AWSController) syncTags() error {
	return nil
}
