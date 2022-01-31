package operator

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	imageregistryv1client "github.com/openshift/client-go/imageregistry/clientset/versioned/typed/imageregistry/v1"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	routeinformers "github.com/openshift/client-go/route/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/events"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
)

// AWSController is for storing internal data required for
// performing AWS controller operations
type AWSController struct {
	infraConfigClient         configv1client.InfrastructureInterface
	imageRegistryConfigClient imageregistryv1client.ConfigInterface
	listers                   *regopclient.Listers

	event        events.Recorder
	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

// NewAWSController is for obtaining AWSController object
// required for invoking AWS controller methods.
func NewAWSController(
	infraConfigClient configv1client.InfrastructureInterface,
	imageRegistryConfigClient imageregistryv1client.ConfigInterface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	regopInformerFactory imageregistryinformers.SharedInformerFactory,
	routeInformerFactory routeinformers.SharedInformerFactory,
	configInformerFactory configinformers.SharedInformerFactory,
	openshiftConfigKubeInformerFactory kubeinformers.SharedInformerFactory,
	openshiftConfigManagedKubeInformerFactory kubeinformers.SharedInformerFactory,
	eventRecorder events.Recorder,
) *AWSController {
	c := &AWSController{
		infraConfigClient:         infraConfigClient,
		imageRegistryConfigClient: imageRegistryConfigClient,
		event:                     eventRecorder,
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"AWSController"),
	}

	infraConfig := configInformerFactory.Config().V1().Infrastructures()
	// list of Listers requied by S3 package NewDriver method
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

// eventHandler is the callback method for handling events from informer
func (c *AWSController) eventHandler() cache.ResourceEventHandler {
	const workQueueKey = "aws"
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(workQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(workQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(workQueueKey) },
	}
}

// Run is the main method for starting the AWS controller
func (c *AWSController) Run(ctx context.Context) {
	defer k8sruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting AWS Controller")
	if !cache.WaitForCacheSync(ctx.Done(), c.cachesToSync...) {
		return
	}

	go wait.Until(c.runWorker, time.Second, ctx.Done())

	klog.Infof("Started AWS Controller")
	<-ctx.Done()
	klog.Infof("Shutting down AWS Controller")
}

func (c *AWSController) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem is for prcessing the event received
// which blocks until a new item is received
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

// sync method is defined for handling the operations required
// on receiving a informer event.
// Fetches image registry config data, required for obtaining
// the S3 bucket configuration and creating a driver out of it
func (c *AWSController) sync() error {
	return c.syncTags()
}

// syncTags fetches user tags from Infrastructure resource, which
// is then compared with the tags configured for the created S3 bucket
// fetched using the driver object passed and updates if any new tags.
func (c *AWSController) syncTags() error {
	cr, err := c.imageRegistryConfigClient.Get(
		context.Background(),
		defaults.ImageRegistryResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		return err
	}

	// if s3 storage config is missing, must be
	// non-AWS platform, so not treating it as error
	if cr.Spec.Storage.S3 == nil {
		return nil
	}

	// make a copy to avoid changing the cached data
	cr = cr.DeepCopy()

	infra, err := c.infraConfigClient.Get(
		context.Background(),
		defaults.InfrastructureResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		klog.Errorf("failed to fetch Infrastructure resource: %v", err)
		return err
	}

	// tags deletion is not supported. Should the user remove it from
	// PlatformSpec, PlatformStatus will be looked up for retaining the tag
	infraTagSet := make(map[string]string)
	mergePlatformSpecStatusTags(infra, infraTagSet)
	klog.Infof("tags read from Infrastructure resource: %v", infraTagSet)

	// Create a driver with the current configuration
	ctx := context.Background()
	driver := s3.NewDriver(ctx, cr.Spec.Storage.S3, c.listers)

	s3TagSet, err := driver.GetStorageTags()
	if err != nil {
		klog.Errorf("failed to fetch storage tags: %v", err)
		return err
	}
	klog.Infof("tags read from storage resource: %v", s3TagSet)

	tagUpdatedCount := compareS3InfraTagSet(s3TagSet, infraTagSet)
	if tagUpdatedCount > 0 {
		if err := driver.PutStorageTags(s3TagSet); err != nil {
			c.event.Warningf("failed to update tags of %s s3 bucket", driver.ID())
			klog.Errorf("failed to update storage tags: %v", err)
		}
		c.event.Eventf("successfully updates tags of %s s3 bucket", driver.ID())
		klog.Infof("successfully added/updated %d tags", tagUpdatedCount)
	}

	return nil
}

// mergePlatformSpecStatusTags is for reading and merging user tags present in both
// Platform Spec and Status of Infrastructure config.
// There could be scenarios(upgrade, user deletes) where user tags could be missing
// from the Platform Spec, hence using Status too to avoid said scenarios.
// If a tag exists in both Status and Spec, Spec is given higher priority.
func mergePlatformSpecStatusTags(infra *configv1.Infrastructure, infraTagSet map[string]string) {
	for _, specTags := range infra.Spec.PlatformSpec.AWS.ResourceTags {
		infraTagSet[specTags.Key] = specTags.Value
	}

	for _, statusTags := range infra.Status.PlatformStatus.AWS.ResourceTags {
		value, ok := infraTagSet[statusTags.Key]
		if !ok {
			klog.Warningf("tag %s exists in infra.Status alone, considering for update",
				statusTags.Key)
			infraTagSet[statusTags.Key] = value
		} else if value != statusTags.Value {
			klog.Warningf("value for tag %s differs in infra.Status(%s) and infra.Spec(%s)"+
				",preferring infra.Spec", statusTags.Key, statusTags.Value, value)
		}
	}
}

// compareS3InfraTagSet is comparing the tags obtained from S3 bucket and user
// to find if any new tags have been added or existing tags modified.
func compareS3InfraTagSet(s3TagSet map[string]string, infraTagSet map[string]string) (tagUpdatedCount int) {
	for key, value := range infraTagSet {
		val, ok := s3TagSet[key]
		if !ok || val != value {
			klog.Infof("%s tag added/updated with value %s", key, value)
			s3TagSet[key] = value
			tagUpdatedCount++
		}
	}
	return
}
