package clusteroperator

import (
	"fmt"

	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"

	osapi "github.com/openshift/cluster-version-operator/pkg/apis/operatorstatus.openshift.io/v1"
	"github.com/operator-framework/operator-sdk/pkg/sdk"

	"github.com/openshift/cluster-image-registry-operator/version"
)

type StatusHandler struct {
	Name      string
	Namespace string
}

func NewStatusHandler(name, namespace string) *StatusHandler {
	return &StatusHandler{
		Name:      name,
		Namespace: namespace,
	}
}

func (s *StatusHandler) Create() error {
	state := &osapi.ClusterOperator{
		TypeMeta: metaapi.TypeMeta{
			APIVersion: osapi.SchemeGroupVersion.String(),
			Kind:       "ClusterOperator",
		},
		ObjectMeta: metaapi.ObjectMeta{
			Name:      s.Name,
			Namespace: s.Namespace,
		},
		Status: osapi.ClusterOperatorStatus{
			Version: version.Version,
		},
	}

	err := sdk.Get(state)
	if !errors.IsNotFound(err) {
		return err
	}

	return sdk.Create(state)
}

func (s *StatusHandler) Update(condtype osapi.ClusterStatusConditionType, status osapi.ConditionStatus, msg string) error {
	state := &osapi.ClusterOperator{
		TypeMeta: metaapi.TypeMeta{
			APIVersion: osapi.SchemeGroupVersion.String(),
			Kind:       "ClusterOperator",
		},
		ObjectMeta: metaapi.ObjectMeta{
			Name:      s.Name,
			Namespace: s.Namespace,
		},
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var sdkFunc func(object sdk.Object) error = sdk.Update

		err := sdk.Get(state)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get cluster operator resource %s/%s: %s", state.ObjectMeta.Namespace, state.ObjectMeta.Name, err)
			}

			state.Status.Conditions = []osapi.ClusterOperatorStatusCondition{
				{
					Type:               osapi.OperatorAvailable,
					Status:             osapi.ConditionUnknown,
					LastTransitionTime: metaapi.Now(),
				},
				{
					Type:               osapi.OperatorProgressing,
					Status:             osapi.ConditionUnknown,
					LastTransitionTime: metaapi.Now(),
				},
				{
					Type:               osapi.OperatorFailing,
					Status:             osapi.ConditionUnknown,
					LastTransitionTime: metaapi.Now(),
				},
			}

			sdkFunc = sdk.Create
		}

		modified := updateOperatorCondition(state, &osapi.ClusterOperatorStatusCondition{
			Type:               condtype,
			Status:             status,
			Message:            msg,
			LastTransitionTime: metaapi.Now(),
		})

		if state.Status.Version != version.Version {
			state.Status.Version = version.Version
			modified = true
		}

		if !modified {
			return nil
		}

		return sdkFunc(state)
	})
}

func updateOperatorCondition(op *osapi.ClusterOperator, condition *osapi.ClusterOperatorStatusCondition) (modified bool) {
	found := false
	conditions := []osapi.ClusterOperatorStatusCondition{}

	for _, c := range op.Status.Conditions {
		if condition.Type != c.Type {
			conditions = append(conditions, c)
			continue
		}
		if condition.Status != c.Status {
			modified = true
		}
		conditions = append(conditions, *condition)
		found = true
	}

	if !found {
		conditions = append(conditions, *condition)
		modified = true
	}

	op.Status.Conditions = conditions
	return
}
