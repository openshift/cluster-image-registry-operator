package operator

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/api/errors"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	kubeset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	operatorapi "github.com/openshift/api/operator/v1"
	configset "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	imageset "github.com/openshift/client-go/image/clientset/versioned"
	routeset "github.com/openshift/client-go/route/clientset/versioned"
	routeinformers "github.com/openshift/client-go/route/informers/externalversions"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusteroperator"
	regopset "github.com/openshift/cluster-image-registry-operator/pkg/generated/clientset/versioned"
	regopinformers "github.com/openshift/cluster-image-registry-operator/pkg/generated/informers/externalversions"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
	"github.com/openshift/cluster-image-registry-operator/pkg/util"
)

const (
	openshiftConfigNamespace = "openshift-config"
	installerConfigNamespace = "kube-system"
	workqueueKey             = "changes"
	defaultResyncDuration    = 10 * time.Minute
)

type permanentError struct {
	Err error
}

func (e permanentError) Error() string {
	return e.Err.Error()
}

func NewController(kubeconfig *restclient.Config) (*Controller, error) {
	namespace, err := regopclient.GetWatchNamespace()
	if err != nil {
		glog.Fatalf("failed to get watch namespace: %s", err)
	}

	p := parameters.Globals{}

	p.Deployment.Namespace = namespace
	p.Deployment.Labels = map[string]string{"docker-registry": "default"}

	p.Pod.ServiceAccount = "registry"
	p.Container.Port = 5000

	p.Healthz.Route = "/healthz"
	p.Healthz.TimeoutSeconds = 5

	p.Service.Name = imageregistryv1.ImageRegistryName
	p.ImageConfig.Name = "cluster"
	p.CAConfig.Name = imageregistryv1.ImageRegistryCertificatesName

	listers := &regopclient.Listers{}
	c := &Controller{
		kubeconfig:    kubeconfig,
		params:        p,
		generator:     resource.NewGenerator(kubeconfig, listers, &p),
		clusterStatus: clusteroperator.NewStatusHandler(kubeconfig, imageregistryv1.ImageRegistryClusterOperatorResourceName),
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Changes"),
		listers:       listers,
	}

	// Initial event to bootstrap CR if it doesn't exist.
	c.workqueue.AddRateLimited(workqueueKey)

	return c, nil
}

type Controller struct {
	kubeconfig    *restclient.Config
	params        parameters.Globals
	generator     *resource.Generator
	clusterStatus *clusteroperator.StatusHandler
	workqueue     workqueue.RateLimitingInterface
	listers       *regopclient.Listers
}

// imageStreamPoll monitors a dummy imagestream to confirm that the openshift
// api server has seen/consumed the internal registry hostname.  This gates
// marking the registry available because this information is considered part
// of the internal registry.
// TODO - it would be better to watch the cluster image config resource and only
// start this polling when we see that resource change (and then stop polling once
// we see the imagestream status reflect the new value) but this polling should
// be low cost.
// NOTE: we can't just watch the imagestreams because the status.dockerrepository value
// is added by a decorator on the imagestream GET api, the resource itself does not
// change so there is no watch event generated when the status.dockerrepository value
// changes.
func (c *Controller) imageStreamPoll() {
	glog.Infof("Polling for imagestream status")
	imageClient, err := imageset.NewForConfig(c.kubeconfig)
	if err != nil {
		glog.Warningf("failed to create client config: %v", err)
		return
	}
	validationIS, err := imageClient.ImageV1().ImageStreams(c.params.Deployment.Namespace).Get("config-validation", metav1.GetOptions{})
	if validationIS == nil || err != nil {
		glog.Warningf("failed to retrieve validation imagestream %s: %s", "config-validation", err)
		// do not change conditions on err to avoid flapping if the api server goes down
		return
	}

	cr, err := c.listers.RegistryConfigs.Get(imageregistryv1.ImageRegistryResourceName)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Warningf("registry operator resource not found")
			return
		}
		glog.Warningf("failed to get %q registry operator resource: %s", imageregistryv1.ImageRegistryResourceName, err)
		return
	}
	cr = cr.DeepCopy() // we don't want to change the cached version
	prevCR := cr.DeepCopy()

	// We want to see the correct value 4 times in a row to ensure all the apiservers are
	// responding with the same value.
	successCount := 0
	for successCount < 4 {
		ic, err := c.listers.ImageConfigs.Get(c.params.ImageConfig.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				glog.Warningf("cluster image config resource %q not found", c.params.ImageConfig.Name)
				return
			}
			glog.Warningf("failed to get %q cluster image config resource: %s", c.params.ImageConfig.Name, err)
			return
		}
		glog.Infof("checking: %s vs %s", validationIS.Status.DockerImageRepository, ic.Status.InternalRegistryHostname)
		if validationIS.Status.DockerImageRepository != "" && strings.HasPrefix(validationIS.Status.DockerImageRepository, ic.Status.InternalRegistryHostname) {
			successCount += 1
			continue
		}
		break
	}

	if successCount >= 4 {
		updateCondition(cr, &operatorapi.OperatorCondition{
			Type:               imageregistryv1.InternalRegistryHostnamePropagated,
			Status:             operatorapi.ConditionStatus(operatorapi.ConditionTrue),
			LastTransitionTime: metaapi.Now(),
		})
	} else {
		updateCondition(cr, &operatorapi.OperatorCondition{
			Type:               imageregistryv1.InternalRegistryHostnamePropagated,
			Status:             operatorapi.ConditionStatus(operatorapi.ConditionFalse),
			LastTransitionTime: metaapi.Now(),
		})
	}

	statusChanged := !reflect.DeepEqual(prevCR.Status, cr.Status)
	if statusChanged {
		glog.Infof("object changed: %s (status=%t)", util.ObjectInfo(cr), statusChanged)

		client, err := regopset.NewForConfig(c.kubeconfig)
		if err != nil {
			glog.Errorf("unable to create client: %s", err)
			return
		}

		_, err = client.ImageregistryV1().Configs().Update(cr)
		if err != nil {
			if !errors.IsConflict(err) {
				glog.Errorf("unable to update %s: %s", util.ObjectInfo(cr), err)
			}
		}
	}
}
func (c *Controller) createOrUpdateResources(cr *imageregistryv1.Config) error {
	appendFinalizer(cr)

	err := verifyResource(cr)
	if err != nil {
		return permanentError{Err: fmt.Errorf("unable to complete resource: %s", err)}
	}

	err = c.generator.Apply(cr)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) sync() error {
	cr, err := c.listers.RegistryConfigs.Get(imageregistryv1.ImageRegistryResourceName)
	if err != nil {
		if errors.IsNotFound(err) {
			return c.Bootstrap()
		}
		return fmt.Errorf("failed to get %q registry operator resource: %s", imageregistryv1.ImageRegistryResourceName, err)
	}
	cr = cr.DeepCopy() // we don't want to change the cached version
	prevCR := cr.DeepCopy()

	if cr.ObjectMeta.DeletionTimestamp != nil {
		return c.finalizeResources(cr)
	}

	var applyError error
	removed := false
	switch cr.Spec.ManagementState {
	case operatorapi.Removed:
		applyError = c.RemoveResources(cr)
		removed = true
	case operatorapi.Managed:
		applyError = c.createOrUpdateResources(cr)
	case operatorapi.Unmanaged:
		// ignore
	default:
		glog.Warningf("unknown custom resource state: %s", cr.Spec.ManagementState)
	}

	deploy, err := c.listers.Deployments.Get(imageregistryv1.ImageRegistryName)
	if errors.IsNotFound(err) {
		deploy = nil
	} else if err != nil {
		return fmt.Errorf("failed to get %q deployment: %s", imageregistryv1.ImageRegistryName, err)
	} else {
		deploy = deploy.DeepCopy() // make sure we won't corrupt the cached vesrion
	}

	c.syncStatus(cr, deploy, applyError, removed)

	metadataChanged := strategy.Metadata(&prevCR.ObjectMeta, &cr.ObjectMeta)
	specChanged := !reflect.DeepEqual(prevCR.Spec, cr.Spec)
	statusChanged := !reflect.DeepEqual(prevCR.Status, cr.Status)
	if metadataChanged || specChanged || statusChanged {
		glog.Infof("object changed: %s (metadata=%t, spec=%t, status=%t)", util.ObjectInfo(cr), metadataChanged, specChanged, statusChanged)

		cr.Status.ObservedGeneration = cr.Generation

		client, err := regopset.NewForConfig(c.kubeconfig)
		if err != nil {
			return err
		}

		_, err = client.ImageregistryV1().Configs().Update(cr)
		if err != nil {
			if !errors.IsConflict(err) {
				glog.Errorf("unable to update %s: %s", util.ObjectInfo(cr), err)
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

		glog.V(1).Infof("get event from workqueue")
		func() {
			defer c.workqueue.Done(obj)

			if _, ok := obj.(string); !ok {
				c.workqueue.Forget(obj)
				glog.Errorf("expected string in workqueue but got %#v", obj)
				return
			}

			if err := c.sync(); err != nil {
				c.workqueue.AddRateLimited(workqueueKey)
				glog.Errorf("unable to sync: %s, requeuing", err)
			} else {
				c.workqueue.Forget(obj)
				glog.Infof("event from workqueue successfully processed")
			}
		}()
	}
}

func (c *Controller) handler() cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(o interface{}) {
			glog.V(1).Infof("add event to workqueue due to %s (add)", util.ObjectInfo(o))
			c.workqueue.Add(workqueueKey)
		},
		UpdateFunc: func(o, n interface{}) {
			newAccessor, err := kmeta.Accessor(n)
			if err != nil {
				glog.Errorf("unable to get accessor for new object: %s", err)
				return
			}
			oldAccessor, err := kmeta.Accessor(o)
			if err != nil {
				glog.Errorf("unable to get accessor for old object: %s", err)
				return
			}
			if newAccessor.GetResourceVersion() == oldAccessor.GetResourceVersion() {
				// Periodic resync will send update events for all known resources.
				// Two different versions of the same resource will always have different RVs.
				return
			}
			glog.V(1).Infof("add event to workqueue due to %s (update)", util.ObjectInfo(n))
			c.workqueue.Add(workqueueKey)
		},
		DeleteFunc: func(o interface{}) {
			object, ok := o.(metaapi.Object)
			if !ok {
				tombstone, ok := o.(cache.DeletedFinalStateUnknown)
				if !ok {
					glog.Errorf("error decoding object, invalid type")
					return
				}
				object, ok = tombstone.Obj.(metaapi.Object)
				if !ok {
					glog.Errorf("error decoding object tombstone, invalid type")
					return
				}
				glog.V(4).Infof("recovered deleted object %q from tombstone", object.GetName())
			}
			glog.V(1).Infof("add event to workqueue due to %s (delete)", util.ObjectInfo(object))
			c.workqueue.Add(workqueueKey)
		},
	}
}

func (c *Controller) Run(stopCh <-chan struct{}) error {
	defer c.workqueue.ShutDown()

	err := c.clusterStatus.Create()
	if err != nil {
		glog.Errorf("unable to create cluster operator resource: %s", err)
	}

	kubeClient, err := kubeset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	routeClient, err := routeset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	configClient, err := configset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	regopClient, err := regopset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	configInformerFactory := configinformers.NewSharedInformerFactory(configClient, defaultResyncDuration)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, defaultResyncDuration, kubeinformers.WithNamespace(c.params.Deployment.Namespace))
	openshiftConfigKubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, defaultResyncDuration, kubeinformers.WithNamespace(openshiftConfigNamespace))
	regopInformerFactory := regopinformers.NewSharedInformerFactory(regopClient, defaultResyncDuration)
	routeInformerFactory := routeinformers.NewSharedInformerFactoryWithOptions(routeClient, defaultResyncDuration, routeinformers.WithNamespace(c.params.Deployment.Namespace))
	installerConfigInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, defaultResyncDuration, kubeinformers.WithNamespace(installerConfigNamespace))

	var informers []cache.SharedIndexInformer
	for _, ctor := range []func() cache.SharedIndexInformer{
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Apps().V1().Deployments()
			c.listers.Deployments = informer.Lister().Deployments(c.params.Deployment.Namespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Apps().V1().DaemonSets()
			c.listers.DaemonSets = informer.Lister().DaemonSets(c.params.Deployment.Namespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().Services()
			c.listers.Services = informer.Lister().Services(c.params.Deployment.Namespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().Secrets()
			c.listers.Secrets = informer.Lister().Secrets(c.params.Deployment.Namespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().ConfigMaps()
			c.listers.ConfigMaps = informer.Lister().ConfigMaps(c.params.Deployment.Namespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := kubeInformerFactory.Core().V1().ServiceAccounts()
			c.listers.ServiceAccounts = informer.Lister().ServiceAccounts(c.params.Deployment.Namespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := routeInformerFactory.Route().V1().Routes()
			c.listers.Routes = informer.Lister().Routes(c.params.Deployment.Namespace)
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
			c.listers.OpenShiftConfig = informer.Lister().ConfigMaps(openshiftConfigNamespace)
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := configInformerFactory.Config().V1().Images()
			c.listers.ImageConfigs = informer.Lister()
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := regopInformerFactory.Imageregistry().V1().Configs()
			c.listers.RegistryConfigs = informer.Lister()
			return informer.Informer()
		},
		func() cache.SharedIndexInformer {
			informer := installerConfigInformerFactory.Core().V1().Secrets()
			c.listers.InstallerSecrets = informer.Lister().Secrets(installerConfigNamespace)
			return informer.Informer()
		},
	} {
		informer := ctor()
		informer.AddEventHandler(c.handler())
		informers = append(informers, informer)
	}

	configInformerFactory.Start(stopCh)
	installerConfigInformerFactory.Start(stopCh)
	kubeInformerFactory.Start(stopCh)
	openshiftConfigKubeInformerFactory.Start(stopCh)
	routeInformerFactory.Start(stopCh)
	configInformerFactory.Start(stopCh)
	regopInformerFactory.Start(stopCh)

	glog.Info("waiting for informer caches to sync")
	for _, informer := range informers {
		if ok := cache.WaitForCacheSync(stopCh, informer.HasSynced); !ok {
			return fmt.Errorf("failed to wait for caches to sync")
		}
	}

	go wait.Until(c.imageStreamPoll, 10*time.Second, stopCh)
	go wait.Until(c.eventProcessor, time.Second, stopCh)

	glog.Info("started events processor")
	<-stopCh
	glog.Info("shutting down events processor")

	return nil
}
