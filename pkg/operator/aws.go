package operator

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
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
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"
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
	featureGateAccessor       featuregates.FeatureGateAccess

	event        events.Recorder
	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

// tagKeyRegex is used to check that the keys and values of a tag contain only valid characters.
var tagKeyRegex = regexp.MustCompile(`^[0-9A-Za-z_.:/=+-@]{1,128}$`)

// tagValRegex is used to check that the keys and values of a tag contain only valid characters.
var tagValRegex = regexp.MustCompile(`^[0-9A-Za-z_.:/=+-@]{0,256}$`)

// kubernetesNamespaceRegex is used to check that a tag key is not in the kubernetes.io namespace.
var kubernetesNamespaceRegex = regexp.MustCompile(`^([^/]*\.)?kubernetes.io/`)

// openshiftNamespaceRegex is used to check that a tag key is not in the openshift.io namespace.
var openshiftNamespaceRegex = regexp.MustCompile(`^([^/]*\.)?openshift.io/`)

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
	featureGateAccessor featuregates.FeatureGateAccess,
) (*AWSController, error) {
	c := &AWSController{
		infraConfigClient:         infraConfigClient,
		imageRegistryConfigClient: imageRegistryConfigClient,
		featureGateAccessor:       featureGateAccessor,
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
		ProxyConfigs:        configInformerFactory.Config().V1().Proxies().Lister(),
		RegistryConfigs:     regopInformerFactory.Imageregistry().V1().Configs().Lister(),
		StorageListers: regopclient.StorageListers{
			Secrets: kubeInformerFactory.Core().V1().Secrets().
				Lister().Secrets(defaults.ImageRegistryOperatorNamespace),
			OpenShiftConfig: openshiftConfigKubeInformerFactory.Core().V1().ConfigMaps().
				Lister().ConfigMaps(defaults.OpenShiftConfigNamespace),
			OpenShiftConfigManaged: openshiftConfigManagedKubeInformerFactory.Core().V1().ConfigMaps().
				Lister().ConfigMaps(defaults.OpenShiftConfigManagedNamespace),
			Infrastructures: infraConfig.Lister(),
		},
	}

	_, err := infraConfig.Informer().AddEventHandler(c.eventHandler())
	if err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, infraConfig.Informer().HasSynced)
	return c, nil
}

// eventHandler is the callback method for handling events from informer
func (c *AWSController) eventHandler() cache.ResourceEventHandler {
	const workQueueKey = "aws"
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			infra, ok := obj.(*configv1.Infrastructure)
			if !ok || infra == nil {
				return
			}
			if infra.Status.PlatformStatus.AWS != nil && len(infra.Status.PlatformStatus.AWS.ResourceTags) != 0 {
				c.queue.Add(workQueueKey)
				return
			}
		},
		UpdateFunc: func(prev, cur interface{}) {
			oldInfra, ok := prev.(*configv1.Infrastructure)
			if !ok || oldInfra == nil {
				return
			}
			newInfra, ok := cur.(*configv1.Infrastructure)
			if !ok || newInfra == nil {
				return
			}
			if oldInfra.Status.PlatformStatus.AWS != nil && newInfra.Status.PlatformStatus.AWS != nil {
				if !reflect.DeepEqual(oldInfra.Status.PlatformStatus.AWS.ResourceTags, newInfra.Status.PlatformStatus.AWS.ResourceTags) {
					c.queue.Add(workQueueKey)
					return
				}
			}
		},
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

	klog.V(5).Infof("AWSController: got event from workqueue")
	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("AWSController: failed to process event: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.V(5).Infof("AWSController: event from workqueue successfully processed")
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
	// PlatformStatus will be looked up for retaining the tag
	infraTagSet := make(map[string]string)
	mergePlatformStatusTags(infra, infraTagSet)
	klog.V(5).Infof("tags read from Infrastructure resource: %v", infraTagSet)

	// Create a driver with the current configuration
	ctx := context.Background()
	driver := s3.NewDriver(ctx, cr.Spec.Storage.S3, &c.listers.StorageListers, c.featureGateAccessor)

	s3TagSet, err := driver.GetStorageTags()
	if err != nil {
		klog.Errorf("failed to fetch storage tags: %v", err)
		return err
	}
	klog.V(5).Infof("tags read from storage resource: %v", s3TagSet)

	tagUpdatedCount := compareS3InfraTagSet(s3TagSet, infraTagSet)
	if tagUpdatedCount > 0 {
		if err := driver.PutStorageTags(s3TagSet); err != nil {
			klog.Errorf("failed to update/delete tagset of %s s3 bucket: %v", driver.ID(), err)
			c.event.Warningf("UpdateAWSTags",
				"Failed to update/delete tagset of %s s3 bucket", driver.ID())
		}
		klog.Infof("successfully updated/deleted %d tags, tagset: %+v", tagUpdatedCount, s3TagSet)
		c.event.Eventf("UpdateAWSTags",
			"Successfully updated/deleted tagset of %s s3 bucket", driver.ID())
	}

	return nil
}

// mergePlatformStatusTags is for reading and merging user tags present in both
// Platform Status of Infrastructure config.
// There could be scenarios(upgrade, user deletes) where user tags could be missing
// from the Platform Status, hence using Status too to avoid said scenarios.
// If a tag exists in Status.
func mergePlatformStatusTags(infra *configv1.Infrastructure, infraTagSet map[string]string) {
	for _, statusTags := range infra.Status.PlatformStatus.AWS.ResourceTags {
		if err := validateUserTag(statusTags.Key, statusTags.Value); err != nil {
			klog.Warningf("validation failed for tag(%s:%s): %v", statusTags.Key, statusTags.Value, err)
			continue
		}
		infraTagSet[statusTags.Key] = statusTags.Value
	}
}

// compareS3InfraTagSet is for comparing the tags obtained from S3 bucket and Infrastructure CR
// to find if any new tags have been deleted, added or existing tags modified.
func compareS3InfraTagSet(s3TagSet map[string]string, infraTagSet map[string]string) (tagUpdatedCount int) {
	for key, value := range infraTagSet {
		// If a tag is value is empty, it's marked for deletion
		// and is deleted from the list obtained from S3 bucket
		if value == "" {
			klog.V(5).Infof("%s tag will be deleted", key)
			delete(s3TagSet, key)
			tagUpdatedCount++
			continue
		}
		val, ok := s3TagSet[key]
		if !ok || val != value {
			klog.V(5).Infof("%s tag will be added/updated with value %s", key, value)
			s3TagSet[key] = value
			tagUpdatedCount++
		}
	}
	return
}

// validateUserTag is for validating the user defined tags in Infrastructure CR
func validateUserTag(key, value string) error {
	if !tagKeyRegex.MatchString(key) {
		return fmt.Errorf("key has invalid characters or length")
	}
	if strings.EqualFold(key, "Name") {
		return fmt.Errorf("key cannot be customized by user")
	}
	if !tagValRegex.MatchString(value) {
		return fmt.Errorf("value has invalid characters or length")
	}
	if kubernetesNamespaceRegex.MatchString(key) {
		return fmt.Errorf("key is in the kubernetes.io namespace")
	}
	if openshiftNamespaceRegex.MatchString(key) {
		return fmt.Errorf("key is in the openshift.io namespace")
	}
	return nil
}
