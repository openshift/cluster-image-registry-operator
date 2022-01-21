package operator

import (
	"context"
	imageregistryv1client "github.com/openshift/client-go/imageregistry/clientset/versioned/typed/imageregistry/v1"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions/imageregistry/v1"
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configv1informers "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configv1listers "github.com/openshift/client-go/config/listers/config/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
)

type AWSController struct {
	configClient         configv1client.ConfigV1Interface
	registryConfigClient imageregistryv1client.ConfigInterface
	infraConfigLister    configv1listers.InfrastructureLister
	registryConfigLister imageregistryv1listers.ConfigLister
	configOperatorClient *regopclient.ConfigOperatorClient

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

func NewAWSController(
	configClient configv1client.ConfigV1Interface,
	registryConfigClient imageregistryv1client.ConfigInterface,
	infraConfigInformer configv1informers.InfrastructureInformer,
	registryConfigInformer imageregistryinformers.ConfigInformer,
	configOperatorClient *regopclient.ConfigOperatorClient,
) *AWSController {
	c := &AWSController{
		configClient:         configClient,
		infraConfigLister:    infraConfigInformer.Lister(),
		registryConfigClient: registryConfigClient,
		registryConfigLister: registryConfigInformer.Lister(),
		configOperatorClient: configOperatorClient,
		queue:                workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AWSController"),
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
	listers := &regopclient.Listers{}
	listers.RegistryConfigs = c.registryConfigLister
	//cr, err := listers.RegistryConfigs.Get(defaults.ImageRegistryResourceName)
	cr, err := c.registryConfigClient.Get(context.Background(),
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
	driver := s3.NewDriver(ctx, cr.Spec.Storage.S3, listers)

	return c.syncTags(driver)
}

func (c *AWSController) syncTags(driver interface{}) error {
	tagset, err := s3.GetStorageTags(driver)
	if err != nil {
		klog.Errorf("syncTags: %v", err)
		return err
	}
	klog.Infof("aws bucket tags: %v", tagset)

	infra, err := c.configClient.Infrastructures().Get(
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
