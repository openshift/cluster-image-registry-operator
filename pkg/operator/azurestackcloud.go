package operator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

type AzureStackCloudController struct {
	operatorClient        v1helpers.OperatorClient
	openshiftConfigLister corev1listers.ConfigMapNamespaceLister

	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

func NewAzureStackCloudController(
	operatorClient v1helpers.OperatorClient,
	openshiftConfigInformer corev1informers.ConfigMapInformer,
) (*AzureStackCloudController, error) {
	c := &AzureStackCloudController{
		operatorClient:        operatorClient,
		openshiftConfigLister: openshiftConfigInformer.Lister().ConfigMaps(defaults.OpenShiftConfigNamespace),
		queue:                 workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AzureStackCloudController"),
	}

	if _, err := openshiftConfigInformer.Informer().AddEventHandler(c.eventHandler()); err != nil {
		return nil, err
	}
	c.cachesToSync = append(c.cachesToSync, openshiftConfigInformer.Informer().HasSynced)

	return c, nil
}

func (c *AzureStackCloudController) eventHandler() cache.ResourceEventHandler {
	const workQueueKey = "instance"
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(workQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(workQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(workQueueKey) },
	}
}

func (c *AzureStackCloudController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *AzureStackCloudController) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(obj)

	klog.V(4).Infof("AzureStackCloudController: got event from workqueue")
	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("AzureStackCloudController: unable to sync: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.V(4).Infof("AzureStackCloudController: event from workqueue successfully processed")
	}
	return true
}

func (c *AzureStackCloudController) getAzureStackCloudConfig() (string, error) {
	cm, err := c.openshiftConfigLister.Get("cloud-provider-config")
	if errors.IsNotFound(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}

	return cm.Data["endpoints"], nil
}

func (c *AzureStackCloudController) syncConfig() error {
	filename := os.Getenv("AZURE_ENVIRONMENT_FILEPATH")
	if filename == "" {
		return fmt.Errorf("AZURE_ENVIRONMENT_FILEPATH is not set")
	}

	config, err := c.getAzureStackCloudConfig()
	if err != nil {
		return err
	}

	if config == "" {
		err = os.Remove(filename)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	f, err := os.CreateTemp(filepath.Dir(filename), "azurestackcloud")
	if err != nil {
		return err
	}

	_, err = f.WriteString(config)
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}

	err = f.Close()
	if err != nil {
		os.Remove(f.Name())
		return err
	}

	err = os.Rename(f.Name(), filename)
	if err != nil {
		os.Remove(f.Name())
		return err
	}

	return nil
}

func (c *AzureStackCloudController) sync() error {
	ctx := context.TODO()
	err := c.syncConfig()
	if err != nil {
		_, _, updateError := v1helpers.UpdateStatus(
			ctx,
			c.operatorClient,
			v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
				Type:    "AzureStackCloudControllerDegraded",
				Status:  operatorv1.ConditionTrue,
				Reason:  "Error",
				Message: err.Error(),
			}))
		return utilerrors.NewAggregate([]error{err, updateError})
	}

	_, _, err = v1helpers.UpdateStatus(
		ctx,
		c.operatorClient,
		v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
			Type:   "AzureStackCloudControllerDegraded",
			Status: operatorv1.ConditionFalse,
			Reason: "AsExpected",
		}))
	return err
}

func (c *AzureStackCloudController) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting AzureStackCloudController")
	if !cache.WaitForCacheSync(ctx.Done(), c.cachesToSync...) {
		return
	}

	go wait.Until(c.runWorker, time.Second, ctx.Done())

	klog.Infof("Started AzureStackCloudController")
	<-ctx.Done()
	klog.Infof("Shutting down AzureStackCloudController")
}
