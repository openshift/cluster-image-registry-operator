package operator

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	batchv1informers "k8s.io/client-go/informers/batch/v1"
	batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchv1listers "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
)

type AzurePathFixController struct {
	batchClient    batchv1client.BatchV1Interface
	operatorClient v1helpers.OperatorClient
	jobLister      batchv1listers.JobNamespaceLister

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

func NewAzurePathFixController(
	batchClient batchv1client.BatchV1Interface,
	operatorClient v1helpers.OperatorClient,
	jobInformer batchv1informers.JobInformer,
) (*AzurePathFixController, error) {
	c := &AzurePathFixController{
		batchClient:    batchClient,
		operatorClient: operatorClient,
		jobLister:      jobInformer.Lister().Jobs(defaults.ImageRegistryOperatorNamespace),
		queue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AzurePathFixController"),
	}

	if _, err := jobInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, jobInformer.Informer().HasSynced)

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
	ctx := context.TODO()
	gen := resource.NewGeneratorAzurePathFixJob(c.jobLister, c.batchClient)

	availableCondition := operatorv1.OperatorCondition{
		Type:   "AzurePathFixAvailable",
		Status: operatorv1.ConditionUnknown,
	}

	progressingCondition := operatorv1.OperatorCondition{
		Type:   "AzurePathFixProgressing",
		Status: operatorv1.ConditionUnknown,
	}

	jobObj, err := gen.Get()
	if errors.IsNotFound(err) {
		availableCondition.Status = operatorv1.ConditionFalse
		availableCondition.Reason = "NotFound"
		availableCondition.Message = "The job does not exist"

		progressingCondition.Status = operatorv1.ConditionTrue
		progressingCondition.Reason = "NotFound"
		progressingCondition.Message = "The job does not exist"
	} else if err != nil {
		availableCondition.Reason = "Unknown"
		availableCondition.Message = fmt.Sprintf("Unable to check job availability: %s", err)

		progressingCondition.Reason = "Unknown"
		progressingCondition.Message = fmt.Sprintf("Unable to check job progress: %s", err)
	} else {
		job := jobObj.(*batchv1.Job)
		job = job

		// TODO
		availableCondition.Reason = "TODO"
		availableCondition.Message = "Something something job"

		progressingCondition.Reason = "TODO"
		progressingCondition.Message = "Something something job"
	}

	err = resource.ApplyMutator(gen)
	if err != nil {
		_, _, updateError := v1helpers.UpdateStatus(
			ctx,
			c.operatorClient,
			v1helpers.UpdateConditionFn(availableCondition),
			v1helpers.UpdateConditionFn(progressingCondition),
			v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
				Type:    "AzurePathFixControllerDegraded",
				Status:  operatorv1.ConditionTrue,
				Reason:  "Error",
				Message: err.Error(),
			}),
		)
		return utilerrors.NewAggregate([]error{err, updateError})
	}

	_, _, err = v1helpers.UpdateStatus(
		ctx,
		c.operatorClient,
		v1helpers.UpdateConditionFn(availableCondition),
		v1helpers.UpdateConditionFn(progressingCondition),
		v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
			Type:   "AzurePathFixControllerDegraded",
			Status: operatorv1.ConditionFalse,
			Reason: "AsExpected",
		}),
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
