package clusteroperator

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	osapi "github.com/openshift/api/config/v1"
	osset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"

	"github.com/openshift/cluster-image-registry-operator/version"
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

	state, err := client.ClusterOperators().Get(s.Name, metaapi.GetOptions{})
	if !errors.IsNotFound(err) {
		return err
	}

	state = &osapi.ClusterOperator{
		ObjectMeta: metaapi.ObjectMeta{
			Name: s.Name,
		},
		Status: osapi.ClusterOperatorStatus{
			Version: version.Version,
		},
	}

	_, err = client.ClusterOperators().Create(state)
	return err
}

func (s *StatusHandler) Update(condtype osapi.ClusterStatusConditionType, status osapi.ConditionStatus, msg string) error {
	client, err := osset.NewForConfig(s.kubeconfig)
	if err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var sdkFunc func(*osapi.ClusterOperator) (*osapi.ClusterOperator, error) = client.ClusterOperators().UpdateStatus

		state, err := client.ClusterOperators().Get(s.Name, metaapi.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get cluster operator resource %s/%s: %s", state.Namespace, state.Name, err)
			}

			state = &osapi.ClusterOperator{
				ObjectMeta: metaapi.ObjectMeta{
					Name: s.Name,
				},
				Status: osapi.ClusterOperatorStatus{
					Conditions: []osapi.ClusterOperatorStatusCondition{
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
					},
				},
			}

			sdkFunc = client.ClusterOperators().Create
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

		_, err = sdkFunc(state)
		return err
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
