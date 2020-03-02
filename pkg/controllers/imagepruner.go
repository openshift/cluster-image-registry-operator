package controllers

import (
	"fmt"
	"reflect"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	batchapi "k8s.io/api/batch/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/wait"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	regopset "github.com/openshift/client-go/imageregistry/clientset/versioned"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/object"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

const (
	imagePrunerWorkQueueKey = "imageprunerchanges"
)

var (
	defaultPrunerSuspend                    = false
	defaultPrunerKeepTagRevisions           = 3
	defaultPrunerSuccessfulJobsHistoryLimit = int32(3)
	defaultPrunerFailedJobsHistoryLimit     = int32(3)
)

// ImagePrunerController keeps track of openshift image pruner components.
type ImagePrunerController struct {
	kubeconfig *restclient.Config
	params     parameters.Globals
	generator  *resource.ImagePrunerGenerator
	workqueue  workqueue.RateLimitingInterface
	listers    *regopclient.Listers
	clients    *regopclient.Clients
	informers  *regopclient.Informers
}

// NewImagePrunerController returns a controller for openshift image pruner.
func NewImagePrunerController(g *regopclient.Generator) (*ImagePrunerController, error) {
	c := &ImagePrunerController{
		kubeconfig: g.Kubeconfig,
		params:     *g.Params,
		generator:  resource.NewImagePrunerGenerator(g.Kubeconfig, g.Clients, g.Listers, g.Params),
		workqueue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), imagePrunerWorkQueueKey),
		listers:    g.Listers,
		clients:    g.Clients,
		informers:  g.Informers,
	}

	// Initial event to bootstrap the pruner if it doesn't exist.
	c.workqueue.AddRateLimited(imagePrunerWorkQueueKey)

	return c, nil
}

func (c *ImagePrunerController) syncPrunerStatus(cr *imageregistryv1.ImagePruner, prunerJob *batchapi.CronJob, lastJobConditions []batchv1.JobCondition) {
	if prunerJob == nil {
		prunerAvailable := operatorapiv1.OperatorCondition{
			Status:  operatorapiv1.ConditionFalse,
			Message: fmt.Sprintf("Pruner CronJob does not exist"),
			Reason:  "Error",
		}
		updatePrunerCondition(cr, operatorapiv1.OperatorStatusTypeAvailable, prunerAvailable)
		metrics.ImagePrunerInstallStatus(false, false)
	} else {
		prunerAvailable := operatorapiv1.OperatorCondition{
			Status:  operatorapiv1.ConditionTrue,
			Message: fmt.Sprintf("Pruner CronJob has been created"),
			Reason:  "Ready",
		}
		updatePrunerCondition(cr, operatorapiv1.OperatorStatusTypeAvailable, prunerAvailable)
	}

	var foundFailed bool
	for _, condition := range lastJobConditions {
		if condition.Type == batchv1.JobFailed {
			foundFailed = true
			prunerLastJobStatus := operatorapiv1.OperatorCondition{
				Status:  operatorapiv1.ConditionTrue,
				Message: condition.Message,
				Reason:  condition.Reason,
			}
			updatePrunerCondition(cr, "Failed", prunerLastJobStatus)

		}
	}
	if !foundFailed {
		prunerLastJobStatus := operatorapiv1.OperatorCondition{
			Status:  operatorapiv1.ConditionFalse,
			Message: fmt.Sprintf("Pruner completed successfully"),
			Reason:  "Complete",
		}
		updatePrunerCondition(cr, "Failed", prunerLastJobStatus)
	}

	if *cr.Spec.Suspend == true {
		prunerJobScheduled := operatorapiv1.OperatorCondition{
			Status:  operatorapiv1.ConditionFalse,
			Message: fmt.Sprintf("The pruner job has been suspended."),
			Reason:  "Suspended",
		}
		updatePrunerCondition(cr, "Scheduled", prunerJobScheduled)
		if prunerJob != nil {
			metrics.ImagePrunerInstallStatus(true, false)
		}
	} else {
		prunerJobScheduled := operatorapiv1.OperatorCondition{
			Status:  operatorapiv1.ConditionTrue,
			Message: fmt.Sprintf("The pruner job has been scheduled."),
			Reason:  "Scheduled",
		}
		updatePrunerCondition(cr, "Scheduled", prunerJobScheduled)
		if prunerJob != nil {
			metrics.ImagePrunerInstallStatus(true, true)
		}
	}
}

func (c *ImagePrunerController) createOrUpdateResources(cr *imageregistryv1.ImagePruner) error {
	return c.generator.Apply(cr)
}

// Bootstrap  creates the initial configuration for the Image Pruner.
func (c *ImagePrunerController) Bootstrap() error {
	cr, err := c.listers.ImagePrunerConfigs.Get(defaults.ImageRegistryImagePrunerResourceName)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("unable to get the registry custom resources: %s", err)
	}

	// If the image pruner resource already exists,
	// no bootstrapping is required
	if cr != nil {
		return nil
	}

	// If no registry resource exists,
	// let's create one with sane defaults
	klog.Infof("generating image pruner custom resource")

	cr = &imageregistryv1.ImagePruner{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.ImageRegistryImagePrunerResourceName,
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Spec: imageregistryv1.ImagePrunerSpec{
			Suspend:                    &defaultPrunerSuspend,
			KeepTagRevisions:           &defaultPrunerKeepTagRevisions,
			SuccessfulJobsHistoryLimit: &defaultPrunerSuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     &defaultPrunerFailedJobsHistoryLimit,
		},
		Status: imageregistryv1.ImagePrunerStatus{},
	}

	client, err := regopset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	_, err = client.ImageregistryV1().ImagePruners().Create(cr)
	if err != nil {
		return err
	}

	return nil
}

func (c *ImagePrunerController) sync() error {
	var applyError error
	pcr, err := c.listers.ImagePrunerConfigs.Get(defaults.ImageRegistryImagePrunerResourceName)
	if err != nil {
		if errors.IsNotFound(err) {
			return c.Bootstrap()
		}
		return fmt.Errorf("failed to get %q image registry pruner resource: %s", defaults.ImageRegistryImagePrunerResourceName, err)

	}

	applyError = c.createOrUpdateResources(pcr)

	pcr = pcr.DeepCopy() // we don't want to change the cached version
	prevPCR := pcr.DeepCopy()
	prunerCronJob, err := c.listers.CronJobs.Get("image-pruner")
	if errors.IsNotFound(err) {
		prunerCronJob = nil
	} else if err != nil {
		return fmt.Errorf("failed to get image-pruner pruner job: %s", err)
	} else {
		prunerCronJob = prunerCronJob.DeepCopy()
	}

	jobSelector := labels.NewSelector()
	requirement, err := labels.NewRequirement("created-by", selection.Equals, []string{"image-pruner"})
	if err != nil {
		return err
	}
	jobSelector.Add(*requirement)
	prunerJobs, err := c.listers.Jobs.List(jobSelector)
	if err != nil {
		return fmt.Errorf("failed to get pruner jobs: %s", err)
	}

	lastPrunerJobConditions := []batchv1.JobCondition{}
	if len(prunerJobs) > 0 {
		sort.Sort(ByCreationTimestamp(prunerJobs))
		lastPrunerJobConditions = prunerJobs[len(prunerJobs)-1].Status.Conditions
	}

	c.syncPrunerStatus(pcr, prunerCronJob, lastPrunerJobConditions)

	metadataChanged := strategy.Metadata(&prevPCR.ObjectMeta, &pcr.ObjectMeta)
	specChanged := !reflect.DeepEqual(prevPCR.Spec, pcr.Spec)
	if metadataChanged || specChanged {
		klog.Infof("Updating pruner cr")
		difference, err := object.DiffString(prevPCR, pcr)
		if err != nil {
			klog.Errorf("unable to calculate difference in %s: %s", utilObjectInfo(pcr), err)
		}
		klog.Infof("object changed: %s (metadata=%t, spec=%t): %s", utilObjectInfo(pcr), metadataChanged, specChanged, difference)

		updatedPCR, err := c.clients.RegOp.ImageregistryV1().ImagePruners().Update(pcr)
		if err != nil {
			if !errors.IsConflict(err) {
				klog.Errorf("unable to update %s: %s", utilObjectInfo(pcr), err)
			}
			return err
		}

		// If we updated the Status field too, we'll make one more call and we
		// want it to succeed.
		pcr.ResourceVersion = updatedPCR.ResourceVersion

	}

	pcr.Status.ObservedGeneration = pcr.Generation
	statusChanged := !reflect.DeepEqual(prevPCR.Status, pcr.Status)
	if statusChanged {
		difference, err := object.DiffString(prevPCR, pcr)
		if err != nil {
			klog.Errorf("unable to calculate difference in %s: %s", utilObjectInfo(pcr), err)
		}
		klog.Infof("object changed: %s (status=%t): %s", utilObjectInfo(pcr), statusChanged, difference)

		_, err = c.clients.RegOp.ImageregistryV1().ImagePruners().UpdateStatus(pcr)
		if err != nil {
			if !errors.IsConflict(err) {
				klog.Errorf("unable to update status %s: %s", utilObjectInfo(pcr), err)
			}
			return err
		}
	}

	if _, ok := applyError.(permanentError); !ok {
		return applyError
	}

	return nil
}

func (c *ImagePrunerController) eventProcessor() {
	for {
		obj, shutdown := c.workqueue.Get()
		if shutdown {
			return
		}

		klog.V(1).Infof("get event from image pruner workqueue")
		func() {
			defer c.workqueue.Done(obj)

			if _, ok := obj.(string); !ok {
				c.workqueue.Forget(obj)
				klog.Errorf("expected string in workqueue but got %#v", obj)
				return
			}

			if err := c.sync(); err != nil {
				c.workqueue.AddRateLimited(imagePrunerWorkQueueKey)
				klog.Errorf("(imaeg pruner) unable to sync: %s, requeuing", err)
			} else {
				c.workqueue.Forget(obj)
				klog.Infof("event from image pruner workqueue successfully processed")
			}
		}()
	}
}

func (c *ImagePrunerController) eventHandler() cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(o interface{}) {
			klog.V(1).Infof("add event to image pruner workqueue due to %s (add)", utilObjectInfo(o))
			c.workqueue.Add(imagePrunerWorkQueueKey)
		},
		UpdateFunc: func(o, n interface{}) {
			newAccessor, err := kmeta.Accessor(n)
			if err != nil {
				klog.Errorf("unable to get accessor for new object: %s", err)
				return
			}
			oldAccessor, err := kmeta.Accessor(o)
			if err != nil {
				klog.Errorf("unable to get accessor for old object: %s", err)
				return
			}
			if newAccessor.GetResourceVersion() == oldAccessor.GetResourceVersion() {
				// Periodic resync will send update events for all known resources.
				// Two different versions of the same resource will always have different RVs.
				return
			}
			klog.V(1).Infof("add event to image pruner workqueue due to %s (update)", utilObjectInfo(n))
			c.workqueue.Add(imagePrunerWorkQueueKey)
		},
		DeleteFunc: func(o interface{}) {
			object, ok := o.(metaapi.Object)
			if !ok {
				tombstone, ok := o.(cache.DeletedFinalStateUnknown)
				if !ok {
					klog.Errorf("error decoding object, invalid type")
					return
				}
				object, ok = tombstone.Obj.(metaapi.Object)
				if !ok {
					klog.Errorf("error decoding object tombstone, invalid type")
					return
				}
				klog.V(4).Infof("recovered deleted object %q from tombstone", object.GetName())
			}
			klog.V(1).Infof("add event to image pruner workqueue due to %s (delete)", utilObjectInfo(object))
			c.workqueue.Add(imagePrunerWorkQueueKey)
		},
	}
}

// Run starts the ImagePrunerController.
func (c *ImagePrunerController) Run(stopCh <-chan struct{}) error {
	defer c.workqueue.ShutDown()

	c.informers.ClusterRoleBindings.AddEventHandler(c.eventHandler())
	c.informers.ClusterRoles.AddEventHandler(c.eventHandler())
	c.informers.CronJobs.AddEventHandler(c.eventHandler())
	c.informers.ImagePrunerConfigs.AddEventHandler(c.eventHandler())
	c.informers.Jobs.AddEventHandler(c.eventHandler())
	c.informers.RegistryConfigs.AddEventHandler(c.eventHandler())
	c.informers.ServiceAccounts.AddEventHandler(c.eventHandler())

	go wait.Until(c.eventProcessor, time.Second, stopCh)

	klog.Info("started image pruner events processor")
	<-stopCh
	klog.Info("shutting down image pruner events processor")

	return nil
}
