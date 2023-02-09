package operator

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	imageregistryclient "github.com/openshift/client-go/imageregistry/clientset/versioned"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	routeclient "github.com/openshift/client-go/route/clientset/versioned"
	routeinformers "github.com/openshift/client-go/route/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/events"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/object"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

const (
	kubeSystemNamespace   = "kube-system"
	workqueueKey          = "changes"
	defaultResyncDuration = 10 * time.Minute
)

type permanentError struct {
	Err    error
	Reason string
}

func newPermanentError(reason string, err error) error {
	return permanentError{
		Err:    err,
		Reason: reason,
	}
}

func (e permanentError) Error() string {
	return e.Err.Error()
}

// NewController returns a controller for openshift image registry objects.
//
// This controller keeps track of resources needed in order to have openshift
// internal registry working.
func NewController(
	eventRecorder events.Recorder,
	kubeconfig *restclient.Config,
	kubeClient kubeclient.Interface,
	configClient configclient.Interface,
	imageregistryClient imageregistryclient.Interface,
	routeClient routeclient.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	openshiftConfigKubeInformerFactory kubeinformers.SharedInformerFactory,
	openshiftConfigManagedKubeInformerFactory kubeinformers.SharedInformerFactory,
	kubeSystemKubeInformerFactory kubeinformers.SharedInformerFactory,
	configInformerFactory configinformers.SharedInformerFactory,
	regopInformerFactory imageregistryinformers.SharedInformerFactory,
	routeInformerFactory routeinformers.SharedInformerFactory,
) (*Controller, error) {
	listers := &regopclient.Listers{}
	clients := &regopclient.Clients{}
	c := &Controller{
		kubeconfig: kubeconfig,
		generator:  resource.NewGenerator(eventRecorder, kubeconfig, clients, listers),
		workqueue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Changes"),
		listers:    listers,
		clients:    clients,
	}

	// Initial event to bootstrap CR if it doesn't exist.
	c.workqueue.AddRateLimited(workqueueKey)

	c.clients.Core = kubeClient.CoreV1()
	c.clients.Apps = kubeClient.AppsV1()
	c.clients.RBAC = kubeClient.RbacV1()
	c.clients.Kube = kubeClient
	c.clients.Route = routeClient.RouteV1()
	c.clients.Config = configClient.ConfigV1()
	c.clients.RegOp = imageregistryClient
	c.clients.Batch = kubeClient.BatchV1()

	for _, ctor := range []func() cache.SharedIndexInformer{
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Apps().V1().Deployments()
			c.listers.Deployments = informer.Lister().Deployments(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().Services()
			c.listers.Services = informer.Lister().Services(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().Secrets()
			c.listers.Secrets = informer.Lister().Secrets(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().ConfigMaps()
			c.listers.ConfigMaps = informer.Lister().ConfigMaps(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().ServiceAccounts()
			c.listers.ServiceAccounts = informer.Lister().ServiceAccounts(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Policy().V1().PodDisruptionBudgets()
			c.listers.PodDisruptionBudgets = informer.Lister().PodDisruptionBudgets(defaults.ImageRegistryOperatorNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := routeInformerFactory.Route().V1().Routes()
			c.listers.Routes = informer.Lister().Routes(defaults.ImageRegistryOperatorNamespace)
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
			informer := openshiftConfigKubeInformerFactory.Core().V1().ConfigMaps()
			c.listers.OpenShiftConfig = informer.Lister().ConfigMaps(defaults.OpenShiftConfigNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := openshiftConfigManagedKubeInformerFactory.Core().V1().ConfigMaps()
			c.listers.OpenShiftConfigManaged = informer.Lister().ConfigMaps(defaults.OpenShiftConfigManagedNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := configInformerFactory.Config().V1().Proxies()
			c.listers.ProxyConfigs = informer.Lister()
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := regopInformerFactory.Imageregistry().V1().Configs()
			c.listers.RegistryConfigs = informer.Lister()
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := configInformerFactory.Config().V1().Infrastructures()
			c.listers.Infrastructures = informer.Lister()
			return informer.Informer()
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

// Controller keeps track of openshift image registry components.
type Controller struct {
	kubeconfig   *restclient.Config
	generator    *resource.Generator
	workqueue    workqueue.RateLimitingInterface
	listers      *regopclient.Listers
	clients      *regopclient.Clients
	cachesToSync []cache.InformerSynced
}

func (c *Controller) createOrUpdateResources(cr *imageregistryv1.Config) error {
	appendFinalizer(cr)

	err := verifyResource(cr)
	if err != nil {
		return newPermanentError("VerificationFailed", fmt.Errorf("unable to complete resource: %s", err))
	}

	err = applyDefaults(cr)
	if err != nil {
		return err
	}

	err = c.generator.Apply(cr)
	if err == storage.ErrStorageNotConfigured {
		return newPermanentError("StorageNotConfigured", err)
	} else if err != nil {
		return err
	}

	return nil
}

// getRoutes returns a list of all routes configured for the image registry, including
// the default route if configured.
func (c *Controller) getRoutes(cr *imageregistryv1.Config) ([]*routev1.Route, error) {
	var routes []*routev1.Route
	if cr.Spec.DefaultRoute {
		if route, err := c.listers.Routes.Get(defaults.RouteName); err != nil {
			klog.V(4).Infof("unable to get default route: %s", err)
		} else {
			routes = append(routes, route)
		}
	}

	for _, rcfg := range cr.Spec.Routes {
		route, err := c.listers.Routes.Get(rcfg.Name)
		if err != nil {
			klog.V(4).Infof("unable to get route: %s", err)
			continue
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func (c *Controller) sync() error {
	cr, err := c.listers.RegistryConfigs.Get(defaults.ImageRegistryResourceName)
	if err != nil {
		if errors.IsNotFound(err) {
			return c.Bootstrap()
		}
		return fmt.Errorf("failed to get %q registry operator resource: %s", defaults.ImageRegistryResourceName, err)
	}
	cr = cr.DeepCopy() // we don't want to change the cached version
	prevCR := cr.DeepCopy()

	if cr.ObjectMeta.DeletionTimestamp != nil {
		err = c.finalizeResources(cr)
		return err
	}

	var applyError error
	switch cr.Spec.ManagementState {
	case operatorv1.Removed:
		applyError = c.RemoveResources(cr)
	case operatorv1.Managed:
		applyError = c.createOrUpdateResources(cr)
	case operatorv1.Unmanaged:
		// ignore
	default:
		klog.Warningf("unknown custom resource state: %s", cr.Spec.ManagementState)
	}

	deploy, err := c.listers.Deployments.Get(defaults.ImageRegistryName)
	if errors.IsNotFound(err) {
		deploy = nil
	} else if err != nil {
		return fmt.Errorf("failed to get %q deployment: %s", defaults.ImageRegistryName, err)
	} else {
		deploy = deploy.DeepCopy() // make sure we won't corrupt the cached vesrion
	}

	routes, err := c.getRoutes(cr)
	if err != nil {
		return err
	}
	c.syncStatus(cr, deploy, routes, applyError)

	metadataChanged := strategy.Metadata(prevCR.ObjectMeta.DeepCopy(), &cr.ObjectMeta)
	specChanged := !reflect.DeepEqual(prevCR.Spec, cr.Spec)
	if metadataChanged || specChanged {
		difference, err := object.DiffString(prevCR, cr)
		if err != nil {
			klog.Errorf("unable to calculate difference in %s: %s", utilObjectInfo(cr), err)
		}
		klog.Infof("object changed: %s (metadata=%t, spec=%t): %s", utilObjectInfo(cr), metadataChanged, specChanged, difference)

		var updatedCR *imageregistryv1.Config
		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			updatedCR, err = c.listers.RegistryConfigs.Get(defaults.ImageRegistryResourceName)
			if err != nil {
				return err
			}
			updatedCR = updatedCR.DeepCopy()

			if metadataChanged {
				updatedCR.ObjectMeta = cr.ObjectMeta
			}
			if specChanged {
				// FIXME: Here be dragons. The operator can
				// accidentally lose user-provided
				// configuration.
				managementState := updatedCR.Spec.ManagementState
				updatedCR.Spec = cr.Spec
				updatedCR.Spec.ManagementState = managementState
			}

			updatedCR, err = c.clients.RegOp.ImageregistryV1().Configs().Update(
				context.TODO(), updatedCR, metaapi.UpdateOptions{},
			)
			return err
		}); err != nil {
			return fmt.Errorf("unable to update config spec: %s", err)
		}

		// If we updated the Status field too, we'll make one more call and we
		// want it to succeed.
		cr.ResourceVersion = updatedCR.ResourceVersion

		// Update prevCR to make diff accurate.
		prevCR.ObjectMeta = updatedCR.ObjectMeta
		prevCR.Spec = updatedCR.Spec
	}

	cr.Status.ObservedGeneration = cr.Generation
	statusChanged := !reflect.DeepEqual(prevCR.Status, cr.Status)
	if statusChanged {
		difference, err := object.DiffString(prevCR, cr)
		if err != nil {
			klog.Errorf("unable to calculate difference in %s: %s", utilObjectInfo(cr), err)
		}
		klog.Infof("object changed: %s (status=%t): %s", utilObjectInfo(cr), statusChanged, difference)

		_, err = c.clients.RegOp.ImageregistryV1().Configs().UpdateStatus(
			context.TODO(), cr, metaapi.UpdateOptions{},
		)
		if err != nil {
			if !errors.IsConflict(err) {
				klog.Errorf("unable to update status %s: %s", utilObjectInfo(cr), err)
			}
			return err
		}
	}

	if _, ok := applyError.(permanentError); !ok {
		return applyError
	}

	return nil
}

func (c *Controller) eventProcessor() {
	for {
		obj, shutdown := c.workqueue.Get()
		if shutdown {
			return
		}

		klog.V(4).Infof("get event from workqueue")
		func() {
			defer c.workqueue.Done(obj)

			if _, ok := obj.(string); !ok {
				c.workqueue.Forget(obj)
				klog.Errorf("expected string in workqueue but got %#v", obj)
				return
			}

			if err := c.sync(); err != nil {
				c.workqueue.AddRateLimited(workqueueKey)
				klog.Errorf("unable to sync: %s, requeuing", err)
			} else {
				c.workqueue.Forget(obj)
				klog.V(4).Infof("event from workqueue successfully processed")
			}
		}()
	}
}

func (c *Controller) handler() cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(o interface{}) {
			obj := o.(metaapi.Object)
			if obj.GetNamespace() == kubeSystemNamespace && obj.GetName() != defaults.ClusterConfigName {
				return
			}
			klog.V(4).Infof("add event to workqueue due to %s (add)", utilObjectInfo(o))
			c.workqueue.Add(workqueueKey)
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
			obj := o.(metaapi.Object)
			if obj.GetNamespace() == kubeSystemNamespace && obj.GetName() != defaults.ClusterConfigName {
				return
			}
			klog.V(4).Infof("add event to workqueue due to %s (update)", utilObjectInfo(n))
			c.workqueue.Add(workqueueKey)
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
			if object.GetNamespace() == kubeSystemNamespace && object.GetName() != defaults.ClusterConfigName {
				return
			}
			klog.V(4).Infof("add event to workqueue due to %s (delete)", utilObjectInfo(object))
			c.workqueue.Add(workqueueKey)
		},
	}
}

// Run starts the Controller.
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	if !cache.WaitForCacheSync(stopCh, c.cachesToSync...) {
		return
	}

	klog.Infof("Starting Controller")
	go wait.Until(c.eventProcessor, time.Second, stopCh)

	<-stopCh
	klog.Infof("Shutting down Controller ...")
}
