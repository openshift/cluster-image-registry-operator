package operator

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	configv1informers "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	imageregistryclient "github.com/openshift/client-go/imageregistry/clientset/versioned"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
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

// NewImagePrunerController returns a controller for openshift image pruner.
func NewImagePrunerController(
	kubeClient kubeclient.Interface,
	imageregistryClient imageregistryclient.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	regopInformerFactory imageregistryinformers.SharedInformerFactory,
	imageConfigInformer configv1informers.ImageInformer,
) (*ImagePrunerController, error) {
	listers := &regopclient.ImagePrunerControllerListers{}
	clients := &regopclient.Clients{}
	c := &ImagePrunerController{
		generator: resource.NewImagePrunerGenerator(clients, listers),
		workqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), imagePrunerWorkQueueKey),
		listers:   listers,
		clients:   clients,
	}

	// Initial event to bootstrap the pruner if it doesn't exist.
	c.workqueue.AddRateLimited(imagePrunerWorkQueueKey)

	c.clients.Core = kubeClient.CoreV1()
	c.clients.Apps = kubeClient.AppsV1()
	c.clients.RBAC = kubeClient.RbacV1()
	c.clients.Kube = kubeClient
	c.clients.RegOp = imageregistryClient
	c.clients.Batch = kubeClient.BatchV1()

	for _, ctor := range []func() cache.SharedIndexInformer{
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().ServiceAccounts()
			c.listers.ServiceAccounts = informer.Lister().ServiceAccounts(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Rbac().V1().ClusterRoles()
			c.listers.ClusterRoles = informer.Lister()
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Rbac().V1().ClusterRoleBindings()
			c.listers.ClusterRoleBindings = informer.Lister()
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := regopInformerFactory.Imageregistry().V1().Configs()
			c.listers.RegistryConfigs = informer.Lister()
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := regopInformerFactory.Imageregistry().V1().ImagePruners()
			c.listers.ImagePrunerConfigs = informer.Lister()
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Batch().V1().CronJobs()
			c.listers.CronJobs = informer.Lister().CronJobs(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Batch().V1().Jobs()
			c.listers.Jobs = informer.Lister().Jobs(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().ConfigMaps()
			c.listers.ConfigMaps = informer.Lister().ConfigMaps(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			c.listers.ImageConfigs = imageConfigInformer.Lister()
			return imageConfigInformer.Informer()
		},
	} {
		informer := ctor()
		if _, err := informer.AddEventHandler(c.handler()); err != nil {
			return nil, err
		}
		c.cachesToSync = append(c.cachesToSync, informer.HasSynced)
	}

	return c, nil
}

// ImagePrunerController keeps track of openshift image pruner components.
type ImagePrunerController struct {
	generator    *resource.ImagePrunerGenerator
	workqueue    workqueue.RateLimitingInterface
	listers      *regopclient.ImagePrunerControllerListers
	clients      *regopclient.Clients
	cachesToSync []cache.InformerSynced
}

func (c *ImagePrunerController) createOrUpdateResources(cr *imageregistryv1.ImagePruner) error {
	return c.generator.Apply(cr)
}

type byCreationTimestamp []*batchv1.Job

func (b byCreationTimestamp) Len() int {
	return len(b)
}

func (b byCreationTimestamp) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func (b byCreationTimestamp) Less(i, j int) bool {
	return b[i].CreationTimestamp.Time.Before(b[j].CreationTimestamp.Time)
}

// Bootstrap creates the initial configuration for the Image Pruner.
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
			Suspend:                      &defaultPrunerSuspend,
			KeepTagRevisions:             &defaultPrunerKeepTagRevisions,
			SuccessfulJobsHistoryLimit:   &defaultPrunerSuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:       &defaultPrunerFailedJobsHistoryLimit,
			IgnoreInvalidImageReferences: true,
		},
		Status: imageregistryv1.ImagePrunerStatus{},
	}

	_, err = c.clients.RegOp.ImageregistryV1().ImagePruners().Create(
		context.TODO(), cr, metav1.CreateOptions{},
	)
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
		sort.Sort(sort.Reverse(byCreationTimestamp(prunerJobs)))
		for _, job := range prunerJobs {
			// skip not finished jobs.
			if len(job.Status.Conditions) == 0 {
				continue
			}
			lastPrunerJobConditions = job.Status.Conditions
			break
		}
	}

	c.syncPrunerStatus(pcr, applyError, prunerCronJob, lastPrunerJobConditions)

	metadataChanged := strategy.Metadata(&prevPCR.ObjectMeta, &pcr.ObjectMeta)
	specChanged := !reflect.DeepEqual(prevPCR.Spec, pcr.Spec)
	if metadataChanged || specChanged {
		klog.Infof("Updating pruner cr")
		difference, err := object.DiffString(prevPCR, pcr)
		if err != nil {
			klog.Errorf("unable to calculate difference in %s: %s", utilObjectInfo(pcr), err)
		}
		klog.Infof("object changed: %s (metadata=%t, spec=%t): %s", utilObjectInfo(pcr), metadataChanged, specChanged, difference)

		updatedPCR, err := c.clients.RegOp.ImageregistryV1().ImagePruners().Update(
			context.TODO(), pcr, metav1.UpdateOptions{},
		)
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

		_, err = c.clients.RegOp.ImageregistryV1().ImagePruners().UpdateStatus(
			context.TODO(), pcr, metav1.UpdateOptions{},
		)
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

		klog.V(4).Infof("get event from image pruner workqueue")
		func() {
			defer c.workqueue.Done(obj)

			if _, ok := obj.(string); !ok {
				c.workqueue.Forget(obj)
				klog.Errorf("expected string in workqueue but got %#v", obj)
				return
			}

			if err := c.sync(); err != nil {
				c.workqueue.AddRateLimited(imagePrunerWorkQueueKey)
				klog.Errorf("(image pruner) unable to sync: %s, requeuing", err)
			} else {
				c.workqueue.Forget(obj)
				klog.V(4).Infof("event from image pruner workqueue successfully processed")
			}
		}()
	}
}

func (c *ImagePrunerController) handler() cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(o interface{}) {
			klog.V(4).Infof("add event to image pruner workqueue due to %s (add)", utilObjectInfo(o))
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
			klog.V(4).Infof("add event to image pruner workqueue due to %s (update)", utilObjectInfo(n))
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
			klog.V(4).Infof("add event to image pruner workqueue due to %s (delete)", utilObjectInfo(object))
			c.workqueue.Add(imagePrunerWorkQueueKey)
		},
	}
}

// Run starts the ImagePrunerController.
func (c *ImagePrunerController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	if !cache.WaitForCacheSync(stopCh, c.cachesToSync...) {
		return
	}

	klog.Infof("Starting ImagePrunerController")
	go wait.Until(c.eventProcessor, time.Second, stopCh)

	<-stopCh
	klog.Infof("Shutting down ImagePrunerController ...")
}
