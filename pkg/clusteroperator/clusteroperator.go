package clusteroperator

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	configapiv1 "github.com/openshift/api/config/v1"
	osset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
)

type StatusHandler struct {
	Name       string
	kubeconfig *restclient.Config
}

func NewStatusHandler(kubeconfig *restclient.Config, name string) *StatusHandler {
	return &StatusHandler{
		Name:       name,
		kubeconfig: kubeconfig,
	}
}

func (s *StatusHandler) Create() error {
	client, err := osset.NewForConfig(s.kubeconfig)
	if err != nil {
		return err
	}

	_, err = client.ClusterOperators().Get(s.Name, metaapi.GetOptions{})
	if !errors.IsNotFound(err) {
		return err
	}

	state := &configapiv1.ClusterOperator{
		ObjectMeta: metaapi.ObjectMeta{
			Name: s.Name,
		},
		Status: configapiv1.ClusterOperatorStatus{},
	}

	_, err = client.ClusterOperators().Create(state)
	return err
}

type ConditionState struct {
	Status  configapiv1.ConditionStatus
	Message string
	Reason  string
}

func (s *StatusHandler) Update(condtype configapiv1.ClusterStatusConditionType, condstate ConditionState, newVersion string) error {
	client, err := osset.NewForConfig(s.kubeconfig)
	if err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var sdkFunc func(*configapiv1.ClusterOperator) (*configapiv1.ClusterOperator, error) = client.ClusterOperators().UpdateStatus
		modified := false
		state, err := client.ClusterOperators().Get(s.Name, metaapi.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get cluster operator resource %s/%s: %s", state.Namespace, state.Name, err)
			}
			modified = true
			state = &configapiv1.ClusterOperator{
				ObjectMeta: metaapi.ObjectMeta{
					Name: s.Name,
				},
				Status: configapiv1.ClusterOperatorStatus{
					Conditions: []configapiv1.ClusterOperatorStatusCondition{
						{
							Type:               configapiv1.OperatorAvailable,
							Status:             configapiv1.ConditionUnknown,
							LastTransitionTime: metaapi.Now(),
						},
						{
							Type:               configapiv1.OperatorProgressing,
							Status:             configapiv1.ConditionUnknown,
							LastTransitionTime: metaapi.Now(),
						},
						{
							Type:               configapiv1.OperatorFailing,
							Status:             configapiv1.ConditionUnknown,
							LastTransitionTime: metaapi.Now(),
						},
					},
				},
			}

			sdkFunc = client.ClusterOperators().Create
		}
		modified = updateOperatorCondition(state, &configapiv1.ClusterOperatorStatusCondition{
			Type:               condtype,
			Status:             condstate.Status,
			Message:            condstate.Message,
			Reason:             condstate.Reason,
			LastTransitionTime: metaapi.Now(),
		})

		if len(newVersion) > 0 {
			newVersions := []configapiv1.OperandVersion{
				{
					Name:    "operator",
					Version: newVersion,
				},
			}
			if !reflect.DeepEqual(state.Status.Versions, newVersions) {
				state.Status.Versions = newVersions
				modified = true
			}
		}

		if !modified {
			return nil
		}

		_, err = sdkFunc(state)
		return err
	})
}

func updateOperatorCondition(op *configapiv1.ClusterOperator, condition *configapiv1.ClusterOperatorStatusCondition) (modified bool) {
	found := false
	conditions := []configapiv1.ClusterOperatorStatusCondition{}

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

func (s *StatusHandler) SetRelatedObjects(refs []configapiv1.ObjectReference) error {
	client, err := osset.NewForConfig(s.kubeconfig)
	if err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		state, err := client.ClusterOperators().Get(s.Name, metaapi.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get cluster operator resource %s/%s: %s", state.Namespace, state.Name, err)
			}

			if err := s.Create(); err != nil {
				return fmt.Errorf("failed to create cluster operator resource %s/%s: %s", state.Namespace, state.Name, err)
			}

			state, err = client.ClusterOperators().Get(s.Name, metaapi.GetOptions{})
			if err != nil {
				return err
			}
		}

		state.Status.RelatedObjects = refs

		_, err = client.ClusterOperators().UpdateStatus(state)
		return err
	})
	return nil
}
