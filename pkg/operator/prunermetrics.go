package operator

import (
	"fmt"
	"time"

	"k8s.io/client-go/tools/cache"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	batchv1informers "k8s.io/client-go/informers/batch/v1"
	batchv1beta1informers "k8s.io/client-go/informers/batch/v1beta1"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

const (
	maxRetries     = 3
	metricsCronKey = "cronjob"
)

type PrunerMetricsController struct {
	workqueue       workqueue.RateLimitingInterface
	ratelimiter     flowcontrol.RateLimiter
	jobInformer     batchv1informers.JobInformer
	cronJobInformer batchv1beta1informers.CronJobInformer
}

func NewPrunerMetricsController(operatorKubeInformerFactory kubeinformers.SharedInformerFactory) *PrunerMetricsController {
	ctrl := &PrunerMetricsController{
		workqueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "PrunerMetrics"),
		jobInformer:     operatorKubeInformerFactory.Batch().V1().Jobs(),
		cronJobInformer: operatorKubeInformerFactory.Batch().V1beta1().CronJobs(),
	}
	ctrl.jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.jobAdded,
		UpdateFunc: ctrl.jobUpdated,
	})
	ctrl.cronJobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ctrl.cronJobAdded,
		UpdateFunc: ctrl.cronJobUpdated,
		DeleteFunc: ctrl.cronJobDeleted,
	})

	return ctrl
}

func NewRateLimitedPrunerMetricsController(operatorKubeInformerFactory kubeinformers.SharedInformerFactory) *PrunerMetricsController {
	ctrl := NewPrunerMetricsController(operatorKubeInformerFactory)
	ctrl.ratelimiter = flowcontrol.NewTokenBucketRateLimiter(1, 4)
	return ctrl
}

func (c *PrunerMetricsController) jobAdded(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.V(5).Infof("Error getting job key: %v", err)
		return
	}
	job := obj.(*batchv1.Job)
	if job.Status.CompletionTime != nil {
		klog.V(5).Infof("Add: enqueing completed job %v", key)
		c.workqueue.AddRateLimited(key)
	}
}

func (c *PrunerMetricsController) jobUpdated(prev, cur interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(cur)
	if err != nil {
		klog.V(5).Infof("Error getting job key: %v", err)
		return
	}
	prevJob := prev.(*batchv1.Job)
	curJob := cur.(*batchv1.Job)
	if prevJob.Status.CompletionTime == nil && curJob.Status.CompletionTime != nil {
		klog.V(5).Infof("Update: enqueing completed job %v", key)
		c.workqueue.AddRateLimited(key)
	}
}

func (c *PrunerMetricsController) cronJobAdded(obj interface{}) {
	klog.Info("added CronJob")
	c.workqueue.AddRateLimited(metricsCronKey)
}

func (c *PrunerMetricsController) cronJobUpdated(prev, cur interface{}) {
	klog.Info("updated CronJob")
	c.workqueue.AddRateLimited(metricsCronKey)
}

func (c *PrunerMetricsController) cronJobDeleted(obj interface{}) {
	klog.Info("deleted CronJob")
	c.workqueue.AddRateLimited(metricsCronKey)
}

func (c *PrunerMetricsController) getJobByKey(jobKey string) (*batchv1.Job, error) {
	obj, exists, err := c.jobInformer.Informer().GetIndexer().GetByKey(jobKey)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	return obj.(*batchv1.Job), nil
}

func (c *PrunerMetricsController) syncPrunerJobMetrics(jobKey string) error {
	err := c.syncInstallationMetric()
	if err != nil {
		return err
	}
	if jobKey == metricsCronKey {
		return nil
	}

	return c.syncJobCompletionMetric(jobKey)
}

func (c *PrunerMetricsController) syncInstallationMetric() error {
	cronJob, err := c.cronJobInformer.Lister().CronJobs(defaults.ImageRegistryOperatorNamespace).Get("image-pruner")
	if err != nil && errors.IsNotFound(err) {
		metrics.ImagePrunerInstallStatus(false, false)
		return nil
	}
	if err != nil {
		return err
	}
	if cronJob.Spec.Suspend != nil && *cronJob.Spec.Suspend == true {
		metrics.ImagePrunerInstallStatus(true, false)
		return nil
	}
	metrics.ImagePrunerInstallStatus(true, true)
	return nil
}

func (c *PrunerMetricsController) syncJobCompletionMetric(jobKey string) error {
	job, err := c.getJobByKey(jobKey)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("job for %s does not exist", jobKey)
	}

	if job.Status.CompletionTime == nil {
		klog.V(6).Infof("Job %s is not complete", jobKey)
		return nil
	}

	if job.Status.Failed > 0 {
		klog.V(6).Infof("Job %s has %d failed jobs - incrementing image pruner failed metric", jobKey, job.Status.Failed)
		metrics.ImagePrunerJobCompleted("failed")
		return nil
	}

	if job.Status.Succeeded > 0 {
		klog.V(6).Infof("Job %s has %d successful jobs - incrementing image pruner succeeded metric", jobKey, job.Status.Succeeded)
		metrics.ImagePrunerJobCompleted("succeeded")
	}
	return nil
}

func (c *PrunerMetricsController) Run(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("Starting PrunerMetricsController")
	go wait.Until(c.metricsWorker, time.Second, stopCh)
	<-stopCh
	klog.Info("Shutting down PrunerMetricsController")
	return nil
}

func (c *PrunerMetricsController) metricsWorker() {
	for c.processNextWorkItem() {

	}
}

func (c *PrunerMetricsController) processNextWorkItem() bool {
	jobKey, quit := c.workqueue.Get()
	if quit {
		return false
	}
	defer c.workqueue.Done(jobKey)

	if c.ratelimiter != nil {
		c.ratelimiter.Accept()
	}
	klog.V(4).Infof("Processing %v", jobKey)
	err := c.syncPrunerJobMetrics(jobKey.(string))
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("%v failed with: %v", jobKey, err))
		if c.workqueue.NumRequeues(jobKey) < maxRetries {
			klog.V(5).Infof("Retrying job %v: %v", jobKey, err)
			c.workqueue.AddRateLimited(jobKey)
			return true
		}
		klog.V(2).Infof("Giving up retrying job %v: %v", jobKey, err)
	}
	c.workqueue.Forget(jobKey)

	return true

}
