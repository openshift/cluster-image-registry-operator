package operator

import (
	"context"
	"fmt"
	"strings"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	batchv1informers "k8s.io/client-go/informers/batch/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchv1listers "k8s.io/client-go/listers/batch/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	configapiv1 "github.com/openshift/api/config/v1"
	configv1informers "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	imageregistryv1informers "github.com/openshift/client-go/imageregistry/informers/externalversions/imageregistry/v1"
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

type AzurePathFixController struct {
	batchClient               batchv1client.BatchV1Interface
	operatorClient            v1helpers.OperatorClient
	jobLister                 batchv1listers.JobNamespaceLister
	imageRegistryConfigLister imageregistryv1listers.ConfigLister
	secretLister              corev1listers.SecretNamespaceLister
	podLister                 corev1listers.PodNamespaceLister
	infrastructureLister      configlisters.InfrastructureLister
	proxyLister               configlisters.ProxyLister
	openshiftConfigLister     corev1listers.ConfigMapNamespaceLister
	kubeconfig                *restclient.Config

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface

	featureGateAccessor featuregates.FeatureGateAccess
}

func NewAzurePathFixController(
	kubeconfig *restclient.Config,
	batchClient batchv1client.BatchV1Interface,
	operatorClient v1helpers.OperatorClient,
	jobInformer batchv1informers.JobInformer,
	imageRegistryConfigInformer imageregistryv1informers.ConfigInformer,
	infrastructureInformer configv1informers.InfrastructureInformer,
	secretInformer corev1informers.SecretInformer,
	proxyInformer configv1informers.ProxyInformer,
	openshiftConfigInformer corev1informers.ConfigMapInformer,
	podInformer corev1informers.PodInformer,
	featureGateAccessor featuregates.FeatureGateAccess,
) (*AzurePathFixController, error) {
	c := &AzurePathFixController{
		batchClient:               batchClient,
		operatorClient:            operatorClient,
		jobLister:                 jobInformer.Lister().Jobs(defaults.ImageRegistryOperatorNamespace),
		imageRegistryConfigLister: imageRegistryConfigInformer.Lister(),
		infrastructureLister:      infrastructureInformer.Lister(),
		secretLister:              secretInformer.Lister().Secrets(defaults.ImageRegistryOperatorNamespace),
		podLister:                 podInformer.Lister().Pods(defaults.ImageRegistryOperatorNamespace),
		proxyLister:               proxyInformer.Lister(),
		openshiftConfigLister:     openshiftConfigInformer.Lister().ConfigMaps(defaults.OpenShiftConfigNamespace),
		kubeconfig:                kubeconfig,
		queue:                     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AzurePathFixController"),
		featureGateAccessor:       featureGateAccessor,
	}

	if _, err := jobInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, jobInformer.Informer().HasSynced)

	if _, err := imageRegistryConfigInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, imageRegistryConfigInformer.Informer().HasSynced)

	if _, err := infrastructureInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, infrastructureInformer.Informer().HasSynced)

	if _, err := secretInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, secretInformer.Informer().HasSynced)

	if _, err := podInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, podInformer.Informer().HasSynced)

	if _, err := proxyInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, proxyInformer.Informer().HasSynced)

	if _, err := openshiftConfigInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, openshiftConfigInformer.Informer().HasSynced)

	// bootstrap the job if it doesn't exist
	c.queue.Add("instance")

	return c, nil
}

func (c *AzurePathFixController) eventHandler() cache.ResourceEventHandler {
	const workQueueKey = "instance"
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(workQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(workQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(workQueueKey) },
	}
}

func (c *AzurePathFixController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *AzurePathFixController) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(obj)

	klog.V(4).Infof("get event from workqueue: %s", obj)

	// the workqueueKey we reference here is different than the one we use in eventHandler
	// use that to identify we are processing an item that was added back to the queue
	// can remove if not useful but curious why this didn't seem to be working for the
	// caches not synced error
	if obj == workqueueKey {
		klog.V(2).Infof("AzurePathFixController processing requeued item  %s", obj)
	}

	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("AzurePathFixController: unable to sync: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.V(4).Infof("AzurePathFixController: event from workqueue successfully processed")
	}
	return true
}

func (c *AzurePathFixController) sync() error {
	// this controller was made to run specifically on Azure,
	// so if we detect a different cloud, skip it.
	infra, err := util.GetInfrastructure(c.infrastructureLister)
	if err != nil {
		return fmt.Errorf("unable to get infrastructure object: %s", err)
	}
	if infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.Type != configapiv1.AzurePlatformType {
		return nil
	}

	imageRegistryConfig, err := c.imageRegistryConfigLister.Get("cluster")
	if err != nil {
		return err
	}

	azureStorage := imageRegistryConfig.Status.Storage.Azure
	if azureStorage == nil || len(azureStorage.AccountName) == 0 || len(azureStorage.Container) == 0 {
		// no need to reque when azure storage isn't configured.
		// this allows customers to use pvc or even emptyDir in azure
		// without getting error messages in loop in the operator logs.
		return nil
	}

	// the move-blobs cmd does not work on Azure Stack Hub. Users on ASH
	// will have to copy the blobs on their own using something like az copy.
	if strings.EqualFold(azureStorage.CloudName, "AZURESTACKCLOUD") {
		return nil
	}

	gen := resource.NewGeneratorAzurePathFixJob(
		c.jobLister,
		c.batchClient,
		c.secretLister,
		c.infrastructureLister,
		c.proxyLister,
		c.openshiftConfigLister,
		imageRegistryConfig,
		c.kubeconfig,
	)

	// this controller was created to aid users migrating from 4.13.z to >=4.14.z.
	// once users have migrated to an OCP version and have run this job at least once,
	// this job is no longer needed. on OCP versions >=4.17 we can be certain that
	// this has already migrated the blobs to the correct place, and we can now
	// safely remove the job. see OCPBUGS-29003 for details.
	progressing := "AzurePathFixProgressing"
	degraded := "AzurePathFixControllerDegraded"
	removeConditionFn := func(conditionType string) v1helpers.UpdateStatusFunc {
		return func(oldStatus *operatorv1.OperatorStatus) error {
			v1helpers.RemoveOperatorCondition(&oldStatus.Conditions, conditionType)
			return nil
		}
	}
	removeConditionFns := []v1helpers.UpdateStatusFunc{}
	progressingConditionFound := v1helpers.FindOperatorCondition(imageRegistryConfig.Status.Conditions, progressing) != nil
	if progressingConditionFound {
		removeConditionFns = append(removeConditionFns, removeConditionFn(progressing))
	}
	degradedConditionFound := v1helpers.FindOperatorCondition(imageRegistryConfig.Status.Conditions, degraded) != nil
	if degradedConditionFound {
		removeConditionFns = append(removeConditionFns, removeConditionFn(degraded))
	}
	if len(removeConditionFns) > 0 {
		if _, _, err := v1helpers.UpdateStatus(
			context.TODO(),
			c.operatorClient,
			removeConditionFns...,
		); err != nil {
			return err
		}
	}

	_, err = gen.Get()
	if errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	} else {
		gracePeriod := int64(0)
		propagationPolicy := metav1.DeletePropagationForeground
		opts := metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriod,
			PropagationPolicy:  &propagationPolicy,
		}
		if err := gen.Delete(opts); err != nil {
			return err
		}
	}
	return err
}

func (c *AzurePathFixController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting AzurePathFixController")
	if !cache.WaitForCacheSync(stopCh, c.cachesToSync...) {
		return
	}

	go wait.Until(c.runWorker, time.Second, stopCh)

	klog.Infof("Started AzurePathFixController")
	<-stopCh
	klog.Infof("Shutting down AzurePathFixController")
}
