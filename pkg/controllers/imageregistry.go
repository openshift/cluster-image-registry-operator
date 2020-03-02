package controllers

import (
	"crypto/rand"
	"fmt"
	"reflect"
	"time"

	appsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	kresource "k8s.io/apimachinery/pkg/api/resource"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	regopset "github.com/openshift/client-go/imageregistry/clientset/versioned/typed/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/object"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/pvc"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/swift"
)

const (
	imageRegistryWorkQueueKey = "imageregistrychanges"

	// randomSecretSize is the number of random bytes to generate
	// for the http secret
	randomSecretSize = 64
)

// Controller keeps track of openshift image registry components.
type Controller struct {
	kubeconfig *restclient.Config
	params     *parameters.Globals
	generator  *resource.Generator
	workqueue  workqueue.RateLimitingInterface
	listers    *regopclient.Listers
	clients    *regopclient.Clients
	informers  *regopclient.Informers
}

// NewController returns a controller for openshift image registry objects.
//
// This controller keeps track of resources needed in order to have openshift
// internal registry working.
func NewController(g *regopclient.Generator) (*Controller, error) {
	c := &Controller{
		kubeconfig: g.Kubeconfig,
		params:     g.Params,
		generator:  resource.NewGenerator(g.Kubeconfig, g.Clients, g.Listers, g.Params),
		workqueue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), imageRegistryWorkQueueKey),
		listers:    g.Listers,
		clients:    g.Clients,
		informers:  g.Informers,
	}

	// Initial event to bootstrap CR if it doesn't exist.
	c.workqueue.AddRateLimited(imageRegistryWorkQueueKey)

	return c, nil
}

func (c *Controller) setStatusRemoving(cr *imageregistryv1.Config) {
	operatorProgressing := operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionTrue,
		Message: "The registry is being removed",
		Reason:  "Removing",
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeProgressing, operatorProgressing)

	operatorProgressing = operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionTrue,
		Message: "The image pruner is being removed",
		Reason:  "Removing",
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeProgressing, operatorProgressing)
}

func (c *Controller) setStatusRemoveFailed(cr *imageregistryv1.Config, removeErr error) {
	operatorDegraded := operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionTrue,
		Message: fmt.Sprintf("Unable to remove registry: %s", removeErr),
		Reason:  "RemoveFailed",
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeDegraded, operatorDegraded)
}

func (c *Controller) syncStatus(cr *imageregistryv1.Config, deploy *appsapi.Deployment, applyError error) {
	operatorAvailable := operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionFalse,
		Message: "",
		Reason:  "",
	}
	if cr.Spec.ManagementState == operatorapiv1.Unmanaged {
		operatorAvailable.Status = operatorapiv1.ConditionTrue
		operatorAvailable.Message = "The registry configuration is set to unmanaged mode"
		operatorAvailable.Reason = "Unmanaged"
	} else if deploy == nil {
		if e, ok := applyError.(permanentError); ok {
			operatorAvailable.Message = applyError.Error()
			operatorAvailable.Reason = e.Reason
		} else if cr.Spec.ManagementState == operatorapiv1.Removed {
			operatorAvailable.Status = operatorapiv1.ConditionTrue
			operatorAvailable.Message = "The registry is removed"
			operatorAvailable.Reason = "Removed"
		} else {
			operatorAvailable.Message = "The deployment does not exist"
			operatorAvailable.Reason = "DeploymentNotFound"
		}
	} else if deploy.DeletionTimestamp != nil {
		operatorAvailable.Message = "The deployment is being deleted"
		operatorAvailable.Reason = "DeploymentDeleted"
	} else if !isDeploymentStatusAvailable(deploy) {
		operatorAvailable.Message = "The deployment does not have available replicas"
		operatorAvailable.Reason = "NoReplicasAvailable"
	} else if !isDeploymentStatusComplete(deploy) {
		operatorAvailable.Status = operatorapiv1.ConditionTrue
		operatorAvailable.Message = "The registry has minimum availability"
		operatorAvailable.Reason = "MinimumAvailability"
	} else {
		operatorAvailable.Status = operatorapiv1.ConditionTrue
		operatorAvailable.Message = "The registry is ready"
		operatorAvailable.Reason = "Ready"
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeAvailable, operatorAvailable)

	operatorProgressing := operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionTrue,
		Message: "",
		Reason:  "",
	}
	if cr.Spec.ManagementState == operatorapiv1.Unmanaged {
		operatorProgressing.Status = operatorapiv1.ConditionFalse
		operatorProgressing.Message = "The registry configuration is set to unmanaged mode"
		operatorProgressing.Reason = "Unmanaged"
	} else if cr.Spec.ManagementState == operatorapiv1.Removed {
		if deploy != nil {
			operatorProgressing.Message = "The deployment is being removed"
			operatorProgressing.Reason = "DeletingDeployment"
		} else {
			operatorProgressing.Status = operatorapiv1.ConditionFalse
			operatorProgressing.Message = "All registry resources are removed"
			operatorProgressing.Reason = "Removed"
		}
	} else if applyError != nil {
		if _, ok := applyError.(permanentError); ok {
			operatorProgressing.Status = operatorapiv1.ConditionFalse
		}
		operatorProgressing.Message = fmt.Sprintf("Unable to apply resources: %s", applyError)
		operatorProgressing.Reason = "Error"
	} else if deploy == nil {
		operatorProgressing.Message = "All resources are successfully applied, but the deployment does not exist"
		operatorProgressing.Reason = "WaitingForDeployment"
	} else if deploy.DeletionTimestamp != nil {
		operatorProgressing.Message = "The deployment is being deleted"
		operatorProgressing.Reason = "FinalizingDeployment"
	} else if !isDeploymentStatusComplete(deploy) {
		operatorProgressing.Message = "The deployment has not completed"
		operatorProgressing.Reason = "DeploymentNotCompleted"
	} else {
		operatorProgressing.Status = operatorapiv1.ConditionFalse
		operatorProgressing.Message = "The registry is ready"
		operatorProgressing.Reason = "Ready"
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeProgressing, operatorProgressing)

	operatorDegraded := operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionFalse,
		Message: "",
		Reason:  "",
	}
	if cr.Spec.ManagementState == operatorapiv1.Unmanaged {
		operatorDegraded.Message = "The registry configuration is set to unmanaged mode"
		operatorDegraded.Reason = "Unmanaged"
	} else if e, ok := applyError.(permanentError); ok {
		operatorDegraded.Status = operatorapiv1.ConditionTrue
		operatorDegraded.Message = applyError.Error()
		operatorDegraded.Reason = e.Reason
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeDegraded, operatorDegraded)

	operatorRemoved := operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionFalse,
		Message: "",
		Reason:  "",
	}
	if cr.Spec.ManagementState == operatorapiv1.Removed {
		operatorRemoved.Status = operatorapiv1.ConditionTrue
		operatorRemoved.Message = "The registry is removed"
		operatorRemoved.Reason = "Removed"
	}

	updateCondition(cr, defaults.OperatorStatusTypeRemoved, operatorRemoved)
}

// Bootstrap registers this operator with OpenShift by creating an appropriate
// ClusterOperator custom resource. This function also creates the initial
// configuration for the Image Registry.
func (c *Controller) Bootstrap() error {
	cr, err := c.listers.RegistryConfigs.Get(defaults.ImageRegistryResourceName)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("unable to get the registry custom resources: %s", err)
	}

	// If the registry resource already exists,
	// no bootstrapping is required
	if cr != nil {
		return nil
	}

	// If no registry resource exists,
	// let's create one with sane defaults
	klog.Infof("generating registry custom resource")

	var secretBytes [randomSecretSize]byte
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
	}

	platformStorage, replicas, err := storage.GetPlatformStorage(c.listers)
	if err != nil {
		return err
	}

	noStorage := imageregistryv1.ImageRegistryConfigStorage{}

	// We bootstrap as "Removed" if the platform is known and does not
	// provide persistent storage out of the box. If the platform is
	// unknown we will bootstrap as Managed but using EmptyDir storage
	// engine(ephemeral).
	mgmtState := operatorapi.Managed
	if platformStorage == noStorage {
		mgmtState = operatorapi.Removed
	}

	infra, err := c.listers.Infrastructures.Get("cluster")
	if err != nil {
		return err
	}

	rolloutStrategy := appsapi.RollingUpdateDeploymentStrategyType

	// If Swift service is not available for OpenStack, we have to start using
	// Cinder with RWO PVC backend. It means that we need to create an RWO claim
	// and set the rollout strategy to Recreate.
	switch infra.Status.PlatformStatus.Type {
	case configapiv1.OpenStackPlatformType:
		isSwiftEnabled, err := swift.IsSwiftEnabled(c.listers)
		if err != nil {
			return err
		}
		if !isSwiftEnabled {
			err = c.createPVC(corev1.ReadWriteOnce)
			if err != nil {
				return err
			}

			rolloutStrategy = appsapi.RecreateDeploymentStrategyType
		}
	}

	cr = &imageregistryv1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:       defaults.ImageRegistryResourceName,
			Namespace:  c.params.Deployment.Namespace,
			Finalizers: []string{parameters.ImageRegistryOperatorResourceFinalizer},
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			ManagementState: mgmtState,
			LogLevel:        2,
			Storage:         platformStorage,
			Replicas:        replicas,
			HTTPSecret:      fmt.Sprintf("%x", string(secretBytes[:])),
			RolloutStrategy: string(rolloutStrategy),
		},
		Status: imageregistryv1.ImageRegistryStatus{},
	}

	client, err := regopset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	_, err = client.Configs().Create(cr)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) createPVC(accessMode corev1.PersistentVolumeAccessMode) error {
	claimName := defaults.PVCImageRegistryName

	// Check that the claim does not exist before creating it
	claim, err := c.clients.Core.PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).Get(claimName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		// "standard" is the default StorageClass name, that was provisioned by the cloud provider
		storageClassName := "standard"

		claim = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      claimName,
				Namespace: defaults.ImageRegistryOperatorNamespace,
				Annotations: map[string]string{
					pvc.PVCOwnerAnnotation: "true",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					accessMode,
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: kresource.MustParse("100Gi"),
					},
				},
				StorageClassName: &storageClassName,
			},
		}

		_, err = c.clients.Core.PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).Create(claim)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) createOrUpdateResources(cr *imageregistryv1.Config) error {
	appendFinalizer(cr)

	err := verifyResource(cr)
	if err != nil {
		return newPermanentError("VerificationFailed", fmt.Errorf("unable to complete resource: %s", err))
	}

	err = c.generator.Apply(cr)
	if err == storage.ErrStorageNotConfigured {
		return newPermanentError("StorageNotConfigured", err)
	} else if err != nil {
		return err
	}

	return nil
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
	case operatorapi.Removed:
		applyError = c.RemoveResources(cr)
	case operatorapi.Managed:
		applyError = c.createOrUpdateResources(cr)
	case operatorapi.Unmanaged:
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

	c.syncStatus(cr, deploy, applyError)

	metadataChanged := strategy.Metadata(&prevCR.ObjectMeta, &cr.ObjectMeta)
	specChanged := !reflect.DeepEqual(prevCR.Spec, cr.Spec)
	if metadataChanged || specChanged {
		difference, err := object.DiffString(prevCR, cr)
		if err != nil {
			klog.Errorf("unable to calculate difference in %s: %s", utilObjectInfo(cr), err)
		}
		klog.Infof("object changed: %s (metadata=%t, spec=%t): %s", utilObjectInfo(cr), metadataChanged, specChanged, difference)

		updatedCR, err := c.clients.RegOp.ImageregistryV1().Configs().Update(cr)
		if err != nil {
			if !errors.IsConflict(err) {
				klog.Errorf("unable to update %s: %s", utilObjectInfo(cr), err)
			}
			return err
		}

		// If we updated the Status field too, we'll make one more call and we
		// want it to succeed.
		cr.ResourceVersion = updatedCR.ResourceVersion
	}

	cr.Status.ObservedGeneration = cr.Generation
	statusChanged := !reflect.DeepEqual(prevCR.Status, cr.Status)
	if statusChanged {
		difference, err := object.DiffString(prevCR, cr)
		if err != nil {
			klog.Errorf("unable to calculate difference in %s: %s", utilObjectInfo(cr), err)
		}
		klog.Infof("object changed: %s (status=%t): %s", utilObjectInfo(cr), statusChanged, difference)

		_, err = c.clients.RegOp.ImageregistryV1().Configs().UpdateStatus(cr)
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

		klog.V(1).Infof("get event from workqueue")
		func() {
			defer c.workqueue.Done(obj)

			if _, ok := obj.(string); !ok {
				c.workqueue.Forget(obj)
				klog.Errorf("expected string in workqueue but got %#v", obj)
				return
			}

			if err := c.sync(); err != nil {
				c.workqueue.AddRateLimited(imageRegistryWorkQueueKey)
				klog.Errorf("unable to sync: %s, requeuing", err)
			} else {
				c.workqueue.Forget(obj)
				klog.Infof("event from workqueue successfully processed")
			}
		}()
	}
}

func (c *Controller) eventHandler() cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(o interface{}) {
			if clusterOperator, ok := o.(*configapiv1.ClusterOperator); ok {
				if clusterOperator.GetName() != defaults.ImageRegistryClusterOperatorResourceName {
					return
				}
			}
			klog.V(1).Infof("add event to workqueue due to %s (add)", utilObjectInfo(o))
			c.workqueue.Add(imageRegistryWorkQueueKey)
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
			if clusterOperator, ok := o.(*configapiv1.ClusterOperator); ok {
				if clusterOperator.GetName() != defaults.ImageRegistryClusterOperatorResourceName {
					return
				}
			}
			klog.V(1).Infof("add event to workqueue due to %s (update)", utilObjectInfo(n))
			c.workqueue.Add(imageRegistryWorkQueueKey)
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
			if clusterOperator, ok := o.(*configapiv1.ClusterOperator); ok {
				if clusterOperator.GetName() != defaults.ImageRegistryClusterOperatorResourceName {
					return
				}
			}
			klog.V(1).Infof("add event to workqueue due to %s (delete)", utilObjectInfo(object))
			c.workqueue.Add(imageRegistryWorkQueueKey)
		},
	}
}

// Run starts the Controller.
func (c *Controller) Run(stopCh <-chan struct{}) error {
	defer c.workqueue.ShutDown()

	c.informers.ClusterOperators.AddEventHandler(c.eventHandler())
	c.informers.ClusterRoleBindings.AddEventHandler(c.eventHandler())
	c.informers.ClusterRoles.AddEventHandler(c.eventHandler())
	c.informers.ConfigMaps.AddEventHandler(c.eventHandler())
	c.informers.CronJobs.AddEventHandler(c.eventHandler())
	c.informers.DaemonSets.AddEventHandler(c.eventHandler())
	c.informers.Deployments.AddEventHandler(c.eventHandler())
	c.informers.ImageConfigs.AddEventHandler(c.eventHandler())
	c.informers.ImagePrunerConfigs.AddEventHandler(c.eventHandler())
	c.informers.Infrastructures.AddEventHandler(c.eventHandler())
	c.informers.Jobs.AddEventHandler(c.eventHandler())
	c.informers.OpenShiftConfig.AddEventHandler(c.eventHandler())
	c.informers.ProxyConfigs.AddEventHandler(c.eventHandler())
	c.informers.RegistryConfigs.AddEventHandler(c.eventHandler())
	c.informers.Routes.AddEventHandler(c.eventHandler())
	c.informers.Secrets.AddEventHandler(c.eventHandler())
	c.informers.ServiceAccounts.AddEventHandler(c.eventHandler())
	c.informers.Services.AddEventHandler(c.eventHandler())

	go wait.Until(c.eventProcessor, time.Second, stopCh)

	klog.Info("started events processor")
	<-stopCh
	klog.Info("shutting down events processor")

	return nil
}

func (c *Controller) RemoveResources(o *imageregistryv1.Config) error {
	c.setStatusRemoving(o)
	return c.generator.Remove(o)
}

func (c *Controller) finalizeResources(o *imageregistryv1.Config) error {
	if o.ObjectMeta.DeletionTimestamp == nil {
		return nil
	}

	finalizers := []string{}
	for _, v := range o.ObjectMeta.Finalizers {
		if v != parameters.ImageRegistryOperatorResourceFinalizer {
			finalizers = append(finalizers, v)
		}
	}

	if len(finalizers) == len(o.ObjectMeta.Finalizers) {
		return nil
	}

	klog.Infof("finalizing %s", utilObjectInfo(o))

	client, err := regopset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	err = c.RemoveResources(o)
	if err != nil {
		c.setStatusRemoveFailed(o, err)
		return fmt.Errorf("unable to finalize resource: %s", err)
	}

	cr := o
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if cr == nil {
			// Skip using the cache here so we don't have as many
			// retries due to slow cache updates
			cr, err := client.Configs().Get(o.Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get %s: %s", utilObjectInfo(o), err)
			}
			finalizers = []string{}
			for _, v := range cr.ObjectMeta.Finalizers {
				if v != parameters.ImageRegistryOperatorResourceFinalizer {
					finalizers = append(finalizers, v)
				}
			}
		}

		cr.ObjectMeta.Finalizers = finalizers

		_, err := client.Configs().Update(cr)
		if err != nil {
			cr = nil
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to update finalizers in %s: %s", utilObjectInfo(o), err)
	}

	// These errors may indicate a transient error that we can retry in tests.
	errorFuncs := []func(error) bool{
		kerrors.IsInternalError,
		kerrors.IsTimeout,
		kerrors.IsServerTimeout,
		kerrors.IsTooManyRequests,
		utilnet.IsProbableEOF,
		utilnet.IsConnectionReset,
	}

	retryTime := 3 * time.Second

	err = wait.PollInfinite(retryTime, func() (stop bool, err error) {
		_, err = c.listers.RegistryConfigs.Get(o.Name)
		if err == nil {
			return
		}

		if !kerrors.IsNotFound(err) {
			for _, isRetryError := range errorFuncs {
				if isRetryError(err) {
					return false, nil
				}
			}

			// If the error sends the Retry-After header, we respect it as an explicit confirmation we should retry.
			if delaySeconds, shouldRetry := kerrors.SuggestsClientDelay(err); shouldRetry {
				delayTime := time.Duration(delaySeconds) * time.Second
				if retryTime < delayTime {
					time.Sleep(delayTime - retryTime)
				}
				return false, nil
			}

			err = fmt.Errorf("failed to get %s: %s", utilObjectInfo(o), err)
			return
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("unable to wait for %s deletion: %s", utilObjectInfo(o), err)
	}

	return nil
}
