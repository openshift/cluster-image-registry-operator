package operator

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	operatorapi "github.com/openshift/api/operator/v1alpha1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	regopset "github.com/openshift/cluster-image-registry-operator/pkg/generated/clientset/versioned/typed/imageregistry/v1alpha1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusteroperator"
	"github.com/openshift/cluster-image-registry-operator/pkg/metautil"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"

	operatorcontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller"
	clusterrolebindingscontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller/clusterrolebindings"
	clusterrolescontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller/clusterroles"
	configmapscontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller/configmaps"
	deploymentscontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller/deployments"
	imageregistrycontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller/imageregistry"
	routescontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller/routes"
	secretscontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller/secrets"
	servicesaccountscontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller/serviceaccounts"
	servicescontroller "github.com/openshift/cluster-image-registry-operator/pkg/operator/controller/services"
)

const (
	WORKQUEUE_KEY = "changes"
)

type permanentError struct {
	Err error
}

func (e permanentError) Error() string {
	return e.Err.Error()
}

func NewController(kubeconfig *restclient.Config, namespace string) (*Controller, error) {
	operatorNamespace, err := regopclient.GetWatchNamespace()
	if err != nil {
		glog.Fatalf("Failed to get watch namespace: %v", err)
	}

	operatorName, err := regopclient.GetOperatorName()
	if err != nil {
		glog.Fatalf("Failed to get operator name: %v", err)
	}

	p := parameters.Globals{}

	p.Deployment.Namespace = namespace
	p.Deployment.Labels = map[string]string{"docker-registry": "default"}

	p.Pod.ServiceAccount = "registry"
	p.Container.Port = 5000

	p.Healthz.Route = "/healthz"
	p.Healthz.TimeoutSeconds = 5

	p.Service.Name = "image-registry"
	p.ImageConfig.Name = "cluster"

	c := &Controller{
		kubeconfig:    kubeconfig,
		params:        p,
		generator:     resource.NewGenerator(kubeconfig, &p),
		clusterStatus: clusteroperator.NewStatusHandler(kubeconfig, operatorName, operatorNamespace),
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Changes"),
	}

	if err = c.Bootstrap(); err != nil {
		return nil, err
	}

	return c, nil
}

type Controller struct {
	kubeconfig    *restclient.Config
	params        parameters.Globals
	generator     *resource.Generator
	clusterStatus *clusteroperator.StatusHandler
	workqueue     workqueue.RateLimitingInterface

	watchers map[string]operatorcontroller.Watcher
}

func (c *Controller) createOrUpdateResources(cr *regopapi.ImageRegistry, modified *bool) error {
	appendFinalizer(cr, modified)

	err := verifyResource(cr, &c.params)
	if err != nil {
		return permanentError{Err: fmt.Errorf("unable to complete resource: %s", err)}
	}

	err = c.generator.Apply(cr, modified)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) CreateOrUpdateResources(cr *regopapi.ImageRegistry, modified *bool) error {
	if cr.Spec.ManagementState != operatorapi.Managed {
		return nil
	}

	return c.createOrUpdateResources(cr, modified)
}

func (c *Controller) Handle(action string, o interface{}) {
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
		glog.V(4).Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}

	objectInfo := fmt.Sprintf("Type=%T ", o)
	if namespace := object.GetNamespace(); namespace != "" {
		objectInfo += fmt.Sprintf("Namespace=%s ", namespace)
	}
	objectInfo += fmt.Sprintf("Name=%s", object.GetName())

	glog.V(1).Infof("Processing %s object %s", action, objectInfo)

	if _, ok := o.(*regopapi.ImageRegistry); !ok {
		ownerRef := metaapi.GetControllerOf(object)
		if ownerRef == nil || ownerRef.Kind != "ImageRegistry" || ownerRef.APIVersion != regopapi.SchemeGroupVersion.String() {
			return
		}
	}

	glog.V(1).Infof("add event to workqueue due to %s (%s)", objectInfo, action)
	c.workqueue.AddRateLimited(WORKQUEUE_KEY)
}

func (c *Controller) sync() error {
	client, err := regopset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	cr, err := client.ImageRegistries().Get(resourceName(c.params.Deployment.Namespace), metaapi.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return c.Bootstrap()
		}
		return fmt.Errorf("failed to get %q custom resource: %s", cr.Name, err)
	}

	if cr.ObjectMeta.DeletionTimestamp != nil {
		return c.finalizeResources(cr)
	}

	var statusChanged bool
	var applyError error
	removed := false
	switch cr.Spec.ManagementState {
	case operatorapi.Removed:
		applyError = c.RemoveResources(cr)
		removed = true
	case operatorapi.Managed:
		applyError = c.CreateOrUpdateResources(cr, &statusChanged)
		if applyError == nil {
			svc, err := c.watchers["services"].Get(c.params.Service.Name, c.params.Deployment.Namespace)
			if err == nil {
				svcObj := svc.(*coreapi.Service)
				svcHostname := fmt.Sprintf("%s.%s.svc:%d", svcObj.Name, svcObj.Namespace, svcObj.Spec.Ports[0].Port)
				if cr.Status.InternalRegistryHostname != svcHostname {
					cr.Status.InternalRegistryHostname = svcHostname
					statusChanged = true
				}
			} else if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get %q service %s", c.params.Service.Name, err)
			}
		}
	case operatorapi.Unmanaged:
		// ignore
	default:
		glog.Warningf("unknown custom resource state: %s", cr.Spec.ManagementState)
	}

	var deployInterface runtime.Object
	deploy, err := c.watchers["deployments"].Get(cr.ObjectMeta.Name, c.params.Deployment.Namespace)
	deployInterface = deploy
	if errors.IsNotFound(err) {
		deployInterface = nil
	} else if err != nil {
		return fmt.Errorf("failed to get %q deployment: %s", cr.ObjectMeta.Name, err)
	}

	c.syncStatus(cr, deployInterface, applyError, removed, &statusChanged)

	if statusChanged {
		glog.Infof("%s changed", metautil.TypeAndName(cr))

		cr.Status.ObservedGeneration = cr.Generation

		_, err = client.ImageRegistries().Update(cr)
		if err != nil {
			if !errors.IsConflict(err) {
				glog.Errorf("unable to update %s: %s", metautil.TypeAndName(cr), err)
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

		err := func(obj interface{}) error {
			defer c.workqueue.Done(obj)

			if _, ok := obj.(string); !ok {
				c.workqueue.Forget(obj)
				glog.Errorf("expected string in workqueue but got %#v", obj)
				return nil
			}

			if err := c.sync(); err != nil {
				c.workqueue.AddRateLimited(WORKQUEUE_KEY)
				return fmt.Errorf("unable to sync: %s, requeuing", err)
			}

			c.workqueue.Forget(obj)

			glog.Infof("event from workqueue successfully processed")
			return nil
		}(obj)

		if err != nil {
			glog.Errorf("unable to process event: %s", err)
		}
	}
}

func (c *Controller) Run(stopCh <-chan struct{}) error {
	defer c.workqueue.ShutDown()

	err := c.clusterStatus.Create()
	if err != nil {
		glog.Errorf("unable to create cluster operator resource: %s", err)
	}

	c.watchers = map[string]operatorcontroller.Watcher{
		"deployments":         &deploymentscontroller.Controller{},
		"services":            &servicescontroller.Controller{},
		"secrets":             &secretscontroller.Controller{},
		"configmaps":          &configmapscontroller.Controller{},
		"servicesaccounts":    &servicesaccountscontroller.Controller{},
		"routes":              &routescontroller.Controller{},
		"clusterroles":        &clusterrolescontroller.Controller{},
		"clusterrolebindings": &clusterrolebindingscontroller.Controller{},
		"imageregistry":       &imageregistrycontroller.Controller{},
	}

	for _, watcher := range c.watchers {
		err = watcher.Start(c.Handle, c.params.Deployment.Namespace, stopCh)
		if err != nil {
			return err
		}
	}

	glog.Info("all controllers are running")

	go wait.Until(c.eventProcessor, time.Second, stopCh)

	glog.Info("started events processor")
	<-stopCh
	glog.Info("shutting down events processor")

	return nil
}
