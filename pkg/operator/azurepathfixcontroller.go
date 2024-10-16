package operator

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configv1informers "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	imageregistryv1informers "github.com/openshift/client-go/imageregistry/informers/externalversions/imageregistry/v1"
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
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

	ctx := context.TODO()
	imageRegistryConfig, err := c.imageRegistryConfigLister.Get("cluster")
	if err != nil {
		return err
	}

	progressing := "AzurePathFixProgressing"
	degraded := "AzurePathFixControllerDegraded"

	// ensure the operator conditions will not remain progressing or degraded
	// because the azure path fix job hasn't completed or has failed.
	// when the operator is set to Removed, it will delete the storage
	// account that it provisioned (unless .spec.storage.managementState is
	// Unmanaged). since the storage account is (or will be) removed, there
	// is no point in reporting the state of the azure path fix job.
	operatorRemoved := imageRegistryConfig.Spec.ManagementState == operatorapiv1.Removed
	storageUnmanaged := imageRegistryConfig.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateUnmanaged
	if operatorRemoved && storageUnmanaged {
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
	}

	azureStorage := imageRegistryConfig.Status.Storage.Azure
	if azureStorage == nil || len(azureStorage.AccountName) == 0 {
		return nil
	}
	if azureStorage == nil || len(azureStorage.Container) == 0 {
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

	progressingCondition := operatorv1.OperatorCondition{
		Type:   progressing,
		Status: operatorv1.ConditionUnknown,
	}
	degradedCondition := operatorv1.OperatorCondition{
		Type:   degraded,
		Status: operatorv1.ConditionFalse,
		Reason: "AsExpected",
	}

	jobObj, err := gen.Get()
	if errors.IsNotFound(err) {
		progressingCondition.Status = operatorv1.ConditionTrue
		progressingCondition.Reason = "NotFound"
		progressingCondition.Message = "The job does not exist"
	} else if err != nil {
		progressingCondition.Reason = "Unknown"
		progressingCondition.Message = fmt.Sprintf("Unable to check job progress: %s", err)
	} else {
		job := jobObj.(*batchv1.Job)
		jobProgressing := true
		var jobCondition batchv1.JobConditionType
		for _, cond := range job.Status.Conditions {
			if (cond.Type == batchv1.JobComplete || cond.Type == batchv1.JobFailed) && cond.Status == corev1.ConditionTrue {
				jobProgressing = false
				jobCondition = cond.Type
				break
			}
		}

		if jobProgressing {
			progressingCondition.Reason = "Migrating"
			progressingCondition.Message = fmt.Sprintf("Azure path fix job is progressing: %d pods active; %d pods failed", job.Status.Active, job.Status.Failed)
			progressingCondition.Status = operatorv1.ConditionTrue
		}

		if jobCondition == batchv1.JobComplete {
			progressingCondition.Reason = "AsExpected"
			progressingCondition.Status = operatorv1.ConditionFalse
		}

		if jobCondition == batchv1.JobFailed {
			progressingCondition.Reason = "Failed"
			progressingCondition.Status = operatorv1.ConditionFalse
			degradedCondition.Reason = "Failed"
			degradedCondition.Status = operatorv1.ConditionTrue

			// if the job still executing (i.e there are attempts left before backoff),
			// we don't want to report degraded, but we let users know that some attempt(s)
			// failed, and the job is still progressing.

			requirement, err := labels.NewRequirement("batch.kubernetes.io/job-name", selection.Equals, []string{gen.GetName()})
			if err != nil {
				// this is extremely unlikely to happen
				return err
			}
			pods, err := c.podLister.List(labels.NewSelector().Add(*requirement))
			if err != nil {
				// there's not much that can be done about an error here,
				// the next reconciliation(s) are likely to succeed.
				return err
			}

			if len(pods) == 0 {
				msg := "Migration failed but no job pods are left to inspect"
				progressingCondition.Message = msg
				degradedCondition.Message = msg
			}

			if len(pods) > 0 {
				mostRecentPod := pods[0]
				for _, pod := range pods {
					if mostRecentPod.CreationTimestamp.Before(&pod.CreationTimestamp) {
						mostRecentPod = pod
					}
				}

				if len(mostRecentPod.Status.ContainerStatuses) > 0 {
					status := mostRecentPod.Status.ContainerStatuses[0]
					msg := fmt.Sprintf("Migration failed: %s", status.State.Terminated.Message)
					progressingCondition.Message = msg
					degradedCondition.Message = msg
				}
			}
		}
	}

	err = resource.ApplyMutator(gen)
	if err != nil {
		_, _, updateError := v1helpers.UpdateStatus(
			ctx,
			c.operatorClient,
			v1helpers.UpdateConditionFn(progressingCondition),
			v1helpers.UpdateConditionFn(degradedCondition),
		)
		return utilerrors.NewAggregate([]error{err, updateError})
	}

	_, _, err = v1helpers.UpdateStatus(
		ctx,
		c.operatorClient,
		v1helpers.UpdateConditionFn(progressingCondition),
		v1helpers.UpdateConditionFn(degradedCondition),
	)
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
