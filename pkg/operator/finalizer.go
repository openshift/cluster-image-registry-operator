package operator

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	osapi "github.com/openshift/api/config/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	regopset "github.com/openshift/cluster-image-registry-operator/pkg/generated/clientset/versioned/typed/imageregistry/v1alpha1"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func (c *Controller) RemoveResources(o *regopapi.ImageRegistry) error {
	modified := false

	errOp := c.clusterStatus.Update(osapi.OperatorProgressing, osapi.ConditionTrue, "registry is being removed")
	if errOp != nil {
		glog.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorProgressing, osapi.ConditionTrue, errOp)
	}

	return c.generator.Remove(o, &modified)
}

func (c *Controller) finalizeResources(o *regopapi.ImageRegistry) error {
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

	glog.Infof("finalizing %s", objectInfo(o))

	err := c.RemoveResources(o)
	if err != nil {
		errOp := c.clusterStatus.Update(osapi.OperatorFailing, osapi.ConditionTrue, "unable to remove registry")
		if errOp != nil {
			glog.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorFailing, osapi.ConditionTrue, errOp)
		}
		return fmt.Errorf("unable to finalize resource: %s", err)
	}

	client, err := regopset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	cr := o
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if cr == nil {
			cr, err := client.ImageRegistries().Get(o.Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get %s: %s", objectInfo(o), err)
			}
			finalizers = []string{}
			for _, v := range cr.ObjectMeta.Finalizers {
				if v != parameters.ImageRegistryOperatorResourceFinalizer {
					finalizers = append(finalizers, v)
				}
			}
		}

		cr.ObjectMeta.Finalizers = finalizers

		_, err := client.ImageRegistries().Update(cr)
		if err != nil {
			cr = nil
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to update finalizers in %s: %s", objectInfo(o), err)
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
		_, err = client.ImageRegistries().Get(o.Name, metav1.GetOptions{})
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

			err = fmt.Errorf("failed to get %s: %s", objectInfo(o), err)
			return
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("unable to wait for %s deletion: %s", objectInfo(o), err)
	}

	return nil
}
