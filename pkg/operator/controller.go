package operator

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/operator-framework/operator-sdk/pkg/util/k8sutil"

	kappsapi "k8s.io/api/apps/v1"
	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"

	operatorapi "github.com/openshift/api/operator/v1alpha1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	osapi "github.com/openshift/cluster-version-operator/pkg/apis/operatorstatus.openshift.io/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/clusteroperator"
	"github.com/openshift/cluster-image-registry-operator/pkg/metautil"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
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
	operatorNamespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		logrus.Fatalf("Failed to get watch namespace: %v", err)
	}

	operatorName, err := k8sutil.GetOperatorName()
	if err != nil {
		logrus.Fatalf("Failed to get operator name: %v", err)
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
		clusterStatus: clusteroperator.NewStatusHandler(operatorName, operatorNamespace),
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Changes"),
	}

	_, err = c.Bootstrap()
	if err != nil {
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
}

func (c *Controller) getImageRegistry() (*regopapi.ImageRegistry, error) {
	cr := &regopapi.ImageRegistry{
		TypeMeta: metaapi.TypeMeta{
			APIVersion: regopapi.SchemeGroupVersion.String(),
			Kind:       "ImageRegistry",
		},
		ObjectMeta: metaapi.ObjectMeta{
			Name: resourceName(c.params.Deployment.Namespace),
		},
	}

	err := sdk.Get(cr)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get %q custom resource: %s", cr.Name, err)
		}
		return nil, nil
	}

	return cr, nil
}

func (c *Controller) createOrUpdateResources(cr *regopapi.ImageRegistry, modified *bool) error {
	appendFinalizer(cr, modified)

	err := verifyResource(cr, &c.params)
	if err != nil {
		return fmt.Errorf("unable to complete resource: %s", err)
	}

	driver, err := storage.NewDriver(cr.Name, c.params.Deployment.Namespace, &cr.Spec.Storage)
	if err == storage.ErrStorageNotConfigured {
		return permanentError{Err: err}
	} else if err != nil {
		return fmt.Errorf("unable to create storage driver: %s", err)
	}

	err = driver.ValidateConfiguration(cr, modified)
	if err != nil {
		return permanentError{Err: fmt.Errorf("invalid configuration: %s", err)}
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

func (c *Controller) Handle(ctx context.Context, event sdk.Event) error {
	logrus.Debugf("received event for %T (deleted=%t)", event.Object, event.Deleted)

	metaObject, ok := event.Object.(metaapi.Object)
	if !ok {
		return nil
	}

	if cr, ok := event.Object.(*regopapi.ImageRegistry); ok {
		if cr.DeletionTimestamp == nil {
			dgst, err := resource.Checksum(cr.Spec)
			if err != nil {
				logrus.Errorf("unable to generate checksum for ImageRegistry spec: %s", err)
				dgst = ""
			}

			curdgst, ok := metaObject.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
			if ok && dgst == curdgst {
				logrus.Debugf("ImageRegistry %s Spec has not changed", metaObject.GetName())
				return nil
			}
		}
	} else {
		ownerRef := metaapi.GetControllerOf(metaObject)

		if ownerRef == nil || ownerRef.Kind != "ImageRegistry" || ownerRef.APIVersion != regopapi.SchemeGroupVersion.String() {
			return nil
		}
	}

	objectInfo := fmt.Sprintf("Type=%T ", event.Object)

	if namespace := metaObject.GetNamespace(); namespace != "" {
		objectInfo += fmt.Sprintf("Namespace=%s ", namespace)
	}
	objectInfo += fmt.Sprintf("Name=%s", metaObject.GetName())

	logrus.Debugf("add event to workqueue due to %s change", objectInfo)
	c.workqueue.AddRateLimited(WORKQUEUE_KEY)

	return nil
}

func (c *Controller) sync() error {
	cr, err := c.getImageRegistry()
	if err != nil {
		return err
	}

	if cr == nil || cr.ObjectMeta.DeletionTimestamp != nil {
		_, err = c.Bootstrap()
		if err != nil {
			return err
		}
		return nil
	}

	var statusChanged bool
	var applyError error
	switch cr.Spec.ManagementState {
	case operatorapi.Removed:
		err = c.RemoveResources(cr)
		if err != nil {
			errOp := c.clusterStatus.Update(osapi.OperatorFailing, osapi.ConditionTrue, "unable to remove registry")
			if errOp != nil {
				logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorFailing, osapi.ConditionTrue, errOp)
			}
			conditionProgressing(cr, operatorapi.ConditionTrue, fmt.Sprintf("unable to remove objects: %s", err), &statusChanged)
		} else {
			conditionRemoved(cr, operatorapi.ConditionTrue, "", &statusChanged)
			conditionAvailable(cr, operatorapi.ConditionFalse, "", &statusChanged)
			conditionProgressing(cr, operatorapi.ConditionFalse, "", &statusChanged)
			conditionFailing(cr, operatorapi.ConditionFalse, "", &statusChanged)
		}
	case operatorapi.Managed:
		conditionRemoved(cr, operatorapi.ConditionFalse, "", &statusChanged)
		applyError = c.CreateOrUpdateResources(cr, &statusChanged)
	case operatorapi.Unmanaged:
		// ignore
	default:
		logrus.Warnf("unknown custom resource state: %s", cr.Spec.ManagementState)
	}

	var deployInterface runtime.Object
	deploy := &kappsapi.Deployment{
		TypeMeta: metaapi.TypeMeta{
			APIVersion: kappsapi.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metaapi.ObjectMeta{
			Name:      cr.ObjectMeta.Name,
			Namespace: c.params.Deployment.Namespace,
		},
	}
	err = sdk.Get(deploy)
	deployInterface = deploy
	if errors.IsNotFound(err) {
		deployInterface = nil
	} else if err != nil {
		return fmt.Errorf("failed to get %q deployment: %s", deploy.Name, err)
	}

	if applyError != nil {
		svc := &coreapi.Service{
			TypeMeta: metaapi.TypeMeta{
				APIVersion: coreapi.SchemeGroupVersion.String(),
				Kind:       "Service",
			},
			ObjectMeta: metaapi.ObjectMeta{
				Name:      c.params.Service.Name,
				Namespace: c.params.Deployment.Namespace,
				Labels:    c.params.Deployment.Labels,
			},
		}
		err = sdk.Get(svc)
		if err == nil {
			svcHostname := fmt.Sprintf("%s.%s.svc.cluster.local:%d", svc.Name, svc.Namespace, svc.Spec.Ports[0].Port)
			if cr.Status.InternalRegistryHostname != svcHostname {
				cr.Status.InternalRegistryHostname = svcHostname
				statusChanged = true
			}
		} else if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get %q service %s", svc.Name, err)
		}
	}

	c.syncStatus(cr, deployInterface, applyError, &statusChanged)

	if statusChanged {
		logrus.Infof("%s changed", metautil.TypeAndName(cr))

		cr.Status.ObservedGeneration = cr.Generation
		addImageRegistryChecksum(cr)

		err = sdk.Update(cr)
		if err != nil && !errors.IsConflict(err) {
			logrus.Errorf("unable to update %s: %s", metautil.TypeAndName(cr), err)
		}
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
				logrus.Errorf("expected string in workqueue but got %#v", obj)
				return nil
			}

			if err := c.sync(); err != nil {
				c.workqueue.AddRateLimited(WORKQUEUE_KEY)
				return fmt.Errorf("unable to sync: %s, requeuing", err)
			}

			c.workqueue.Forget(obj)

			logrus.Infof("workqueue successfully synced")
			return nil
		}(obj)

		if err != nil {
			logrus.Errorf("unable to process event: %s", err)
		}
	}
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	defer c.workqueue.ShutDown()

	err := c.clusterStatus.Create()
	if err != nil {
		logrus.Errorf("unable to create cluster operator resource: %s", err)
	}

	go wait.Until(c.eventProcessor, time.Second, stopCh)

	logrus.Info("started events processor")
	<-stopCh
	logrus.Info("shutting down events processor")
}
