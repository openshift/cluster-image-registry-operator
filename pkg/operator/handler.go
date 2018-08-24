package operator

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	operatorapi "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewHandler() sdk.Handler {
	return &Handler{}
}

type Handler struct {
}

func conditionResourceValid(cr *v1alpha1.OpenShiftDockerRegistry, status operatorapi.ConditionStatus, m string) {
	cr.Status.Conditions = append(cr.Status.Conditions,
		operatorapi.OperatorCondition{
			Type:               operatorapi.OperatorStatusTypeAvailable,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             "ResourceValidation",
			Message:            m,
		},
	)
}

func conditionResourceApply(cr *v1alpha1.OpenShiftDockerRegistry, status operatorapi.ConditionStatus, m string) {
	cr.Status.Conditions = append(cr.Status.Conditions,
		operatorapi.OperatorCondition{
			Type:               operatorapi.OperatorStatusTypeAvailable,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             "ResourceApply",
			Message:            m,
		},
	)
}

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.OpenShiftDockerRegistry:
		// Ignore the delete event since the garbage collector will clean up all secondary resources for the CR
		// All secondary resources must have the CR set as their OwnerReference for this to be the case
		if event.Deleted {
			return nil
		}

		if o.Spec.ManagementState != operatorapi.Managed {
			return nil
		}

		statusChanged, err := applyResource(o)
		if err != nil {
			return err
		}

		if statusChanged {
			err := sdk.Update(o)
			if err != nil {
				logrus.Errorf("unable to update registry custom resource: %s", err)
			}
		}
	}
	return nil
}

func applyResource(o *v1alpha1.OpenShiftDockerRegistry) (bool, error) {
	o.Status.Conditions = []operatorapi.OperatorCondition{}

	dc, err := GenerateDeploymentConfig(o)
	if err != nil {
		msg := fmt.Sprintf("unable to make deployment config: %s", err)

		logrus.Error(msg)
		conditionResourceValid(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	modified := false

	err = ApplyServiceAccount(GenerateServiceAccount(o, dc), &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to apply service account: %s", err)

		logrus.Error(msg)
		conditionResourceApply(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	err = ApplyClusterRoleBinding(GenerateClusterRoleBinding(o, dc), &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to apply cluster role binding: %s", err)

		logrus.Error(msg)
		conditionResourceApply(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	err = ApplyService(GenerateService(o, dc), &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to apply service: %s", err)

		logrus.Error(msg)
		conditionResourceApply(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	err = ApplyDeploymentConfig(dc, &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to apply deployment config: %s", err)

		logrus.Error(msg)
		conditionResourceApply(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	if modified {
		logrus.Infof("registry resources changed")

		conditionResourceApply(o, operatorapi.ConditionTrue, "all resources applied")

		err = sdk.Update(o)
		if err != nil {
			logrus.Errorf("unable to update registry custom resource: %s", err)
			return modified, err
		}
	}

	return modified, nil
}
