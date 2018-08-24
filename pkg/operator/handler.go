package operator

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	operatorapi "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
)

func NewHandler() sdk.Handler {
	return &Handler{}
}

type Handler struct {
}

func conditionReourceValid(cr *v1alpha1.OpenShiftDockerRegistry, status operatorapi.ConditionStatus, m string) {
	cr.Status.Conditions = append(cr.Status.Conditions,
		operatorapi.OperatorCondition{
			Type:    operatorapi.OperatorStatusTypeAvailable,
			Status:  status,
			Reason:  "ResourceValidation",
			Message: m,
		},
	)
}

func conditionReourceApply(cr *v1alpha1.OpenShiftDockerRegistry, status operatorapi.ConditionStatus, m string) {
	cr.Status.Conditions = append(cr.Status.Conditions,
		operatorapi.OperatorCondition{
			Type:    operatorapi.OperatorStatusTypeAvailable,
			Status:  status,
			Reason:  "ResourceApply",
			Message: m,
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

		o.Status.Conditions = []operatorapi.OperatorCondition{}

		dc, err := GenerateDeploymentConfig(o)
		if err != nil {
			msg := fmt.Sprintf("unable to make deployment config: %s", err)

			logrus.Error(msg)
			conditionReourceValid(o, operatorapi.ConditionFalse, msg)

			return nil
		}

		err = ApplyServiceAccount(GenerateServiceAccount(o, dc))
		if err != nil {
			msg := fmt.Sprintf("unable to apply service account: %s", err)

			logrus.Error(msg)
			conditionReourceApply(o, operatorapi.ConditionFalse, msg)

			return nil
		}

		err = ApplyClusterRoleBinding(GenerateClusterRoleBinding(o, dc))
		if err != nil {
			msg := fmt.Sprintf("unable to apply cluster role binding: %s", err)

			logrus.Error(msg)
			conditionReourceApply(o, operatorapi.ConditionFalse, msg)

			return nil
		}

		err = ApplyService(GenerateService(o, dc))
		if err != nil {
			msg := fmt.Sprintf("unable to apply service: %s", err)

			logrus.Error(msg)
			conditionReourceApply(o, operatorapi.ConditionFalse, msg)

			return nil
		}

		err = ApplyDeploymentConfig(dc)
		if err != nil {
			msg := fmt.Sprintf("unable to apply deployment config: %s", err)

			logrus.Error(msg)
			conditionReourceApply(o, operatorapi.ConditionFalse, msg)

			return nil
		}

		conditionReourceApply(o, operatorapi.ConditionTrue, "")

		// TODO update status
	}
	return nil
}
