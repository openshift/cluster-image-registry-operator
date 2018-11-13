package operator

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/metautil"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
)

func (h *Handler) RemoveResources(o *regopapi.ImageRegistry) error {
	modified := false

	templetes, err := resource.Templates(o, &h.params)
	if err != nil {
		return fmt.Errorf("unable to generate templates: %s", err)
	}

	for _, tmpl := range templetes {
		err = resource.RemoveByTemplate(tmpl, &modified)
		if err != nil {
			return fmt.Errorf("unable to remove objects: %s", err)
		}
		logrus.Infof("resource %s removed", tmpl.Name())
	}

	configState, err := resource.GetConfigState(h.params.Deployment.Namespace)
	if err != nil {
		return fmt.Errorf("unable to get previous config state: %s", err)
	}

	err = resource.RemoveConfigState(configState)
	if err != nil {
		return fmt.Errorf("unable to remove previous config state: %s", err)
	}

	return nil
}

func (h *Handler) finalizeResources(o *regopapi.ImageRegistry) error {
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

	logrus.Infof("finalizing %s", metautil.TypeAndName(o))

	err := h.RemoveResources(o)
	if err != nil {
		return err
	}

	cr := o
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if cr == nil {
			cr = &regopapi.ImageRegistry{
				TypeMeta: metav1.TypeMeta{
					APIVersion: o.TypeMeta.APIVersion,
					Kind:       o.TypeMeta.Kind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      o.Name,
					Namespace: o.Namespace,
				},
			}

			err := sdk.Get(cr)
			if err != nil {
				return fmt.Errorf("failed to get %s: %s", metautil.TypeAndName(o), err)
			}

			finalizers = []string{}
			for _, v := range cr.ObjectMeta.Finalizers {
				if v != parameters.ImageRegistryOperatorResourceFinalizer {
					finalizers = append(finalizers, v)
				}
			}
		}

		cr.ObjectMeta.Finalizers = finalizers

		err := sdk.Update(cr)
		if err != nil {
			cr = nil
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("unable to update finalizers in %s: %s", metautil.TypeAndName(o), err)
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
		cr = &regopapi.ImageRegistry{
			TypeMeta: metav1.TypeMeta{
				APIVersion: o.TypeMeta.APIVersion,
				Kind:       o.TypeMeta.Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      o.Name,
				Namespace: o.Namespace,
			},
		}

		err = sdk.Get(cr)
		if err == nil {
			return
		}

		if !kerrors.IsNotFound(err) {
			for _, isRetryError := range errorFuncs {
				if isRetryError(err) {
					return
				}
			}

			// If the error sends the Retry-After header, we respect it as an explicit confirmation we should retry.
			if delaySeconds, shouldRetry := kerrors.SuggestsClientDelay(err); shouldRetry {
				delayTime := time.Duration(delaySeconds) * time.Second
				if retryTime < delayTime {
					time.Sleep(delayTime - retryTime)
				}
				return
			}

			err = fmt.Errorf("failed to get %s: %s", metautil.TypeAndName(o), err)
			return
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("unable to wait for %s deletion: %s", metautil.TypeAndName(o), err)
	}

	return nil
}
