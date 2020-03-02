package controllers

import (
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

const (
	clusterOperatorWorkQueueKey = "clusteroperatorchanges"
)

type ClusterOperatorStatusController struct {
	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
	params       *parameters.Globals
	// These fields should be used only for NewGenerator
	kubeconfig *rest.Config
	clients    *client.Clients
	listers    *client.Listers
	informers  *client.Informers
}

func NewClusterOperatorStatusController(g *regopclient.Generator) (*ClusterOperatorStatusController, error) {
	c := &ClusterOperatorStatusController{
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), clusterOperatorWorkQueueKey),
		kubeconfig: g.Kubeconfig,
		params:     g.Params,
		clients:    g.Clients,
		listers:    g.Listers,
		informers:  g.Informers,
	}

	return c, nil
}

func (c *ClusterOperatorStatusController) eventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(clusterOperatorWorkQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(clusterOperatorWorkQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(clusterOperatorWorkQueueKey) },
	}
}

func (c *ClusterOperatorStatusController) eventProcessor() {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return
	}
	defer c.queue.Done(obj)

	klog.V(1).Infof("get event from workqueue")
	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(clusterOperatorWorkQueueKey)
		klog.Errorf("unable to sync ClusterOperatorStatusController: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.Infof("event from workqueue successfully processed")
	}
}

func (c *ClusterOperatorStatusController) sync() error {
	cr, err := c.listers.RegistryConfigs.Get("cluster")
	if err != nil {
		return err
	}
	cr = cr.DeepCopy()

	gen := resource.NewGenerator(c.kubeconfig, c.clients, c.listers, c.params)
	resources, err := gen.List(cr)
	if err != nil && err != storage.ErrStorageNotConfigured {
		return err
	}

	mut := resource.NewGeneratorClusterOperator(
		c.listers,
		c.clients,
		cr,
		resources,
	)

	return resource.ApplyMutator(mut)
}

func (c *ClusterOperatorStatusController) Run(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.informers.ClusterOperators.AddEventHandler(c.eventHandler())
	c.informers.Deployments.AddEventHandler(c.eventHandler())
	c.informers.RegistryConfigs.AddEventHandler(c.eventHandler())

	go wait.Until(c.eventProcessor, time.Second, stopCh)

	klog.Infof("Started ClusterOperatorStatusController")
	<-stopCh
	klog.Infof("Shutting down ClusterOperatorStatusController")

	return nil
}
