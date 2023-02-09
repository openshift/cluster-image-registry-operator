package operator

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	configapi "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	routev1informers "github.com/openshift/client-go/route/informers/externalversions/route/v1"
	routev1lister "github.com/openshift/client-go/route/listers/route/v1"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
)

// ImageConfigController controls image.config.openshift.io/cluster.
//
// Watches for changes on image registry routes and services, updating
// the resource status appropriately.
type ImageConfigController struct {
	configClient   configset.ConfigV1Interface
	operatorClient v1helpers.OperatorClient
	routeLister    routev1lister.RouteNamespaceLister
	serviceLister  corev1listers.ServiceNamespaceLister
	cachesToSync   []cache.InformerSynced
	queue          workqueue.RateLimitingInterface
}

func NewImageConfigController(
	configClient configset.ConfigV1Interface,
	operatorClient v1helpers.OperatorClient,
	routeInformer routev1informers.RouteInformer,
	serviceInformer corev1informers.ServiceInformer,
) (*ImageConfigController, error) {
	icc := &ImageConfigController{
		configClient:   configClient,
		operatorClient: operatorClient,
		routeLister:    routeInformer.Lister().Routes(defaults.ImageRegistryOperatorNamespace),
		serviceLister:  serviceInformer.Lister().Services(defaults.ImageRegistryOperatorNamespace),
		queue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ImageConfigController"),
	}

	if _, err := serviceInformer.Informer().AddEventHandler(icc.eventHandler()); err != nil {
		return nil, err
	}
	icc.cachesToSync = append(icc.cachesToSync, serviceInformer.Informer().HasSynced)

	if _, err := routeInformer.Informer().AddEventHandler(icc.eventHandler()); err != nil {
		return nil, err
	}
	icc.cachesToSync = append(icc.cachesToSync, routeInformer.Informer().HasSynced)

	return icc, nil
}

func (icc *ImageConfigController) eventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { icc.queue.Add("instance") },
		UpdateFunc: func(old, new interface{}) { icc.queue.Add("instance") },
		DeleteFunc: func(obj interface{}) { icc.queue.Add("instance") },
	}
}

func (icc *ImageConfigController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer icc.queue.ShutDown()

	klog.Infof("Starting ImageConfigController")
	if !cache.WaitForCacheSync(stopCh, icc.cachesToSync...) {
		return
	}

	go wait.Until(icc.runWorker, time.Second, stopCh)

	klog.Infof("Started ImageConfigController")
	<-stopCh
	klog.Infof("Shutting down ImageConfigController")
}

func (icc *ImageConfigController) runWorker() {
	for icc.processNextWorkItem() {
	}
}

func (icc *ImageConfigController) processNextWorkItem() bool {
	obj, shutdown := icc.queue.Get()
	if shutdown {
		return false
	}
	defer icc.queue.Done(obj)

	klog.V(4).Infof("get event from workqueue")
	if err := icc.sync(); err != nil {
		icc.queue.AddRateLimited(workqueueKey)
		klog.Errorf("ImageConfigController: unable to sync: %s, requeuing", err)
	} else {
		icc.queue.Forget(obj)
		klog.V(4).Infof("ImageConfigController: event from workqueue processed")
	}

	return true
}

// sync keeps image.config.openshift.io/cluster status updated.
func (icc *ImageConfigController) syncImageStatus() error {
	cfg, err := icc.configClient.Images().Get(context.TODO(), defaults.ImageConfigName, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if errors.IsNotFound(err) {
		if cfg, err = icc.configClient.Images().Create(
			context.TODO(),
			&configapi.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name: defaults.ImageConfigName,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}
	}

	externalHostnames, err := icc.getRouteHostnames()
	if err != nil {
		return err
	}

	internalHostname, err := icc.getServiceHostname()
	if err != nil {
		return err
	}

	modified := false
	if !reflect.DeepEqual(externalHostnames, cfg.Status.ExternalRegistryHostnames) {
		cfg.Status.ExternalRegistryHostnames = externalHostnames
		modified = true
	}
	if cfg.Status.InternalRegistryHostname != internalHostname {
		cfg.Status.InternalRegistryHostname = internalHostname
		modified = true
	}

	if modified {
		if _, err := icc.configClient.Images().UpdateStatus(context.TODO(), cfg, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func (icc *ImageConfigController) sync() error {
	ctx := context.TODO()
	err := icc.syncImageStatus()
	if err != nil {
		_, _, updateError := v1helpers.UpdateStatus(
			ctx,
			icc.operatorClient,
			v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
				Type:    "ImageConfigControllerDegraded",
				Status:  operatorv1.ConditionTrue,
				Reason:  "Error",
				Message: err.Error(),
			}))
		return utilerrors.NewAggregate([]error{err, updateError})
	}

	_, _, err = v1helpers.UpdateStatus(
		ctx,
		icc.operatorClient,
		v1helpers.UpdateConditionFn(operatorv1.OperatorCondition{
			Type:   "ImageConfigControllerDegraded",
			Status: operatorv1.ConditionFalse,
			Reason: "AsExpected",
		}))
	return err
}

// getServiceHostname returns the image registry internal service url if it
// exists, empty string is returned otherwise.
func (icc *ImageConfigController) getServiceHostname() (string, error) {
	svc, err := icc.serviceLister.Get(defaults.ServiceName)
	if errors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	port := ""
	if svc.Spec.Ports[0].Port != 443 {
		port = fmt.Sprintf(":%d", svc.Spec.Ports[0].Port)
	}

	return fmt.Sprintf("%s.%s.svc%s", svc.Name, svc.Namespace, port), nil
}

// getRouteHostnames returns all image registry exposed routes.
func (icc *ImageConfigController) getRouteHostnames() ([]string, error) {
	var routeNames []string

	routes, err := icc.routeLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	defaultHost := ""
	for _, route := range routes {
		if !resource.RouteIsCreatedByOperator(route) {
			continue
		}

		for _, ingress := range route.Status.Ingress {
			hostname := ingress.Host
			if len(hostname) == 0 {
				continue
			}

			defaultHostPrefix := fmt.Sprintf(
				"%s-%s",
				defaults.RouteName,
				defaults.ImageRegistryOperatorNamespace,
			)
			if strings.HasPrefix(hostname, defaultHostPrefix) {
				defaultHost = hostname
				continue
			}

			routeNames = append(routeNames, hostname)
		}
	}

	// ensure a stable order for these values so we don't cause flapping in the
	// downstream controllers that watch this array
	sort.Strings(routeNames)

	// make sure the default route hostname comes first in the list because the
	// first entry will be used as the public repository hostname by the cluster
	// configuration
	if len(defaultHost) > 0 {
		routeNames = append([]string{defaultHost}, routeNames...)
	}

	return routeNames, nil
}
