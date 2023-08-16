package operator

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
)

type NodeCADaemonController struct {
	eventRecorder   events.Recorder
	appsClient      appsv1client.AppsV1Interface
	operatorClient  v1helpers.OperatorClient
	daemonSetLister appsv1listers.DaemonSetNamespaceLister
	serviceLister   corev1listers.ServiceNamespaceLister

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

func NewNodeCADaemonController(
	eventRecorder events.Recorder,
	appsClient appsv1client.AppsV1Interface,
	operatorClient v1helpers.OperatorClient,
	daemonSetInformer appsv1informers.DaemonSetInformer,
	serviceInformer corev1informers.ServiceInformer,
) (*NodeCADaemonController, error) {
	c := &NodeCADaemonController{
		eventRecorder:   eventRecorder,
		appsClient:      appsClient,
		operatorClient:  operatorClient,
		daemonSetLister: daemonSetInformer.Lister().DaemonSets(defaults.ImageRegistryOperatorNamespace),
		serviceLister:   serviceInformer.Lister().Services(defaults.ImageRegistryOperatorNamespace),
		queue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "NodeCADaemonController"),
	}

	if _, err := daemonSetInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, daemonSetInformer.Informer().HasSynced)

	if _, err := serviceInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, serviceInformer.Informer().HasSynced)

	return c, nil
}

func (c *NodeCADaemonController) eventHandler() cache.ResourceEventHandler {
	const workQueueKey = "instance"
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(workQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(workQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(workQueueKey) },
	}
}

func (c *NodeCADaemonController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *NodeCADaemonController) processNextWorkItem() bool {
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
		klog.V(2).Infof("NodeCADaemonController processing requeued item  %s", obj)
	}

	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("NodeCADaemonController: unable to sync: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.V(4).Infof("NodeCADaemonController: event from workqueue successfully processed")
	}
	return true
}

func (c *NodeCADaemonController) sync() error {
	ctx := context.TODO()
	gen := resource.NewGeneratorNodeCADaemonSet(c.eventRecorder, c.daemonSetLister, c.serviceLister, c.appsClient, c.operatorClient)

	availableCondition := operatorv1.OperatorCondition{
		Type:   "NodeCADaemonAvailable",
		Status: operatorv1.ConditionUnknown,
	}

	progressingCondition := operatorv1.OperatorCondition{
		Type:   "NodeCADaemonProgressing",
		Status: operatorv1.ConditionUnknown,
	}

	dsObj, err := gen.Get()
	if errors.IsNotFound(err) {
		availableCondition.Status = operatorv1.ConditionFalse
		availableCondition.Reason = "NotFound"
		availableCondition.Message = "The daemon set node-ca does not exist"

		progressingCondition.Status = operatorv1.ConditionTrue
		progressingCondition.Reason = "NotFound"
		progressingCondition.Message = "The daemon set node-ca does not exist"
	} else if err != nil {
		availableCondition.Reason = "Unknown"
		availableCondition.Message = fmt.Sprintf("Unable to check daemon set availability: %s", err)

		progressingCondition.Reason = "Unknown"
		progressingCondition.Message = fmt.Sprintf("Unable to check daemon set progress: %s", err)
	} else {
		ds := dsObj.(*appsv1.DaemonSet)
		if ds.Status.NumberAvailable > 0 {
			availableCondition.Status = operatorv1.ConditionTrue
			availableCondition.Reason = "AsExpected"
			availableCondition.Message = "The daemon set node-ca has available replicas"
		} else {
			availableCondition.Status = operatorv1.ConditionFalse
			availableCondition.Reason = "NoAvailableReplicas"
			availableCondition.Message = "The daemon set node-ca does not have available replicas"
		}

		if ds.Generation != ds.Status.ObservedGeneration {
			progressingCondition.Status = operatorv1.ConditionTrue
			progressingCondition.Reason = "Progressing"
			progressingCondition.Message = "The daemon set node-ca is updating node pods"
		} else if ds.Status.NumberUnavailable > 0 {
			progressingCondition.Status = operatorv1.ConditionTrue
			progressingCondition.Reason = "Unavailable"
			progressingCondition.Message = "The daemon set node-ca is deploying node pods"
		} else {
			progressingCondition.Status = operatorv1.ConditionFalse
			progressingCondition.Reason = "AsExpected"
			progressingCondition.Message = "The daemon set node-ca is deployed"
		}
	}

	err = resource.ApplyMutator(gen)
	if err != nil {
		_, _, updateError := v1helpers.UpdateStatus(
			ctx,
			c.operatorClient,
			v1helpers.UpdateConditionFn(availableCondition),
			v1helpers.UpdateConditionFn(progressingCondition),
			v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
				Type:    "NodeCADaemonControllerDegraded",
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
			Type:   "NodeCADaemonControllerDegraded",
			Status: operatorv1.ConditionFalse,
			Reason: "AsExpected",
		}),
	)
	return err
}

func (c *NodeCADaemonController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting NodeCADaemonController")
	if !cache.WaitForCacheSync(stopCh, c.cachesToSync...) {
		return
	}

	go wait.Until(c.runWorker, time.Second, stopCh)

	klog.Infof("Started NodeCADaemonController")
	<-stopCh
	klog.Infof("Shutting down NodeCADaemonController")
}
