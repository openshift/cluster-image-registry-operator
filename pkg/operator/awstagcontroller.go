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

// AWSTagController is for storing internal data required for
// performing AWS controller operations
type AWSTagController struct {
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

// NewAWSTagController is for obtaining AWSTagController object
// required for invoking AWS Tag controller methods.
func NewAWSTagController(
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
) (*AWSTagController, error) {
	c := &AWSTagController{
		infraConfigClient:         infraConfigClient,
		imageRegistryConfigClient: imageRegistryConfigClient,
		featureGateAccessor:       featureGateAccessor,
		event:                     eventRecorder,
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"AWSTagController"),
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
func (c *AWSTagController) eventHandler() cache.ResourceEventHandler {
	const workQueueKey = "aws"
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			infra, ok := obj.(*configv1.Infrastructure)
			if !ok || infra == nil {
				return
			}
			if infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.AWS != nil &&
				len(infra.Status.PlatformStatus.AWS.ResourceTags) != 0 {
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
			if oldInfra.Status.PlatformStatus != nil && oldInfra.Status.PlatformStatus.AWS != nil &&
				newInfra.Status.PlatformStatus != nil && newInfra.Status.PlatformStatus.AWS != nil {
				if !reflect.DeepEqual(oldInfra.Status.PlatformStatus.AWS.ResourceTags, newInfra.Status.PlatformStatus.AWS.ResourceTags) {
					c.queue.Add(workQueueKey)
					return
				}
			}
		},
	}
}

// Run is the main method for starting the AWS controller
func (c *AWSTagController) Run(ctx context.Context) {
	defer k8sruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting AWS Tag Controller")
	if !cache.WaitForCacheSync(ctx.Done(), c.cachesToSync...) {
		return
	}

	go wait.Until(c.runWorker, time.Second, ctx.Done())

	klog.Infof("Started AWS Tag Controller")
	<-ctx.Done()
	klog.Infof("Shutting down AWS Tag Controller")
}

func (c *AWSTagController) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem is for prcessing the event received
// which blocks until a new item is received
func (c *AWSTagController) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(obj)

	klog.V(5).Infof("AWSTagController: got event from workqueue")
	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("AWSTagController: failed to process event: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.V(5).Infof("AWSTagController: event from workqueue successfully processed")
	}
	return true
}

// sync method is defined for handling the operations required
// on receiving a informer event.
// Fetches image registry config data, required for obtaining
// the S3 bucket configuration and creating a driver out of it
func (c *AWSTagController) sync() error {
	return c.syncTags()
}

// syncTags fetches user tags from Infrastructure resource, which
// is then compared with the tags configured for the created S3 bucket
// fetched using the driver object passed and updates if any new tags.
func (c *AWSTagController) syncTags() error {
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

	// Filtering tags based on validation
	infraTagSet := filterPlatformStatusTags(infra)
	klog.V(5).Infof("tags read from Infrastructure resource: %v", infraTagSet)

	// Create a driver with the current configuration
	ctx := context.Background()
	driver := s3.NewDriver(ctx, cr.Spec.Storage.S3, &c.listers.StorageListers, c.featureGateAccessor)

	s3TagSet, err := driver.GetStorageTags()
	if err != nil {
		klog.Errorf("failed to fetch storage tags: %v", err)
		return err
	}
	klog.Infof("tags read from storage resource: %v", s3TagSet)

	tagUpdatedCount := syncInfraTags(s3TagSet, infraTagSet)
	if tagUpdatedCount > 0 {
		if err := driver.PutStorageTags(s3TagSet); err != nil {
			klog.Errorf("failed to update/append tagset of %s s3 bucket: %v", driver.ID(), err)
			c.event.Warningf("UpdateAWSTags",
				"Failed to update/append tagset of %s s3 bucket", driver.ID())
		}
		klog.Infof("successfully updated/appended %d tags, tagset: %+v", tagUpdatedCount, s3TagSet)
		c.event.Eventf("UpdateAWSTags",
			"Successfully updated tagset of %s s3 bucket", driver.ID())
	}

	return nil
}

// filterPlatformStatusTags is for reading and filter user tags present in
// Platform Status of Infrastructure config.
func filterPlatformStatusTags(infra *configv1.Infrastructure) map[string]string {
	infraTagSet := map[string]string{}
	for _, statusTags := range infra.Status.PlatformStatus.AWS.ResourceTags {
		if err := validateUserTag(statusTags.Key, statusTags.Value); err != nil {
			klog.Warningf("validation failed for tag(%s:%s): %v", statusTags.Key, statusTags.Value, err)
			continue
		}
		infraTagSet[statusTags.Key] = statusTags.Value
	}
	return infraTagSet
}

// syncInfraTags synchronizes the tags obtained from S3 bucket and Infrastructure CR.
// this modifies the s3TagSet based on new tags which are added and update the value to a key if it has changed.
func syncInfraTags(s3TagSet map[string]string, infraTagSet map[string]string) int {
	tagUpdatedCount := 0
	for key, value := range infraTagSet {
		val, ok := s3TagSet[key]
		if !ok || val != value {
			klog.V(5).Infof("%s tag will be added/updated with value %s", key, value)
			s3TagSet[key] = value
			tagUpdatedCount++
		}
	}
	return tagUpdatedCount
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
