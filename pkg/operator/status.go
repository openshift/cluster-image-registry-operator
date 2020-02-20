package operator

import (
	"fmt"

	appsapi "k8s.io/api/apps/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
)

func updateCondition(cr *imageregistryv1.Config, condtype string, condstate operatorapiv1.OperatorCondition) {
	found := false
	conditions := []operatorapiv1.OperatorCondition{}

	for _, c := range cr.Status.Conditions {
		if c.Type != condtype {
			conditions = append(conditions, c)
			continue
		}
		if c.Status != condstate.Status {
			c.Status = condstate.Status
			c.LastTransitionTime = metaapi.Now()
		}
		if c.Reason != condstate.Reason {
			c.Reason = condstate.Reason
		}
		if c.Message != condstate.Message {
			c.Message = condstate.Message
		}
		conditions = append(conditions, c)
		found = true
	}

	if !found {
		conditions = append(conditions, operatorapiv1.OperatorCondition{
			Type:               condtype,
			Status:             operatorapiv1.ConditionStatus(condstate.Status),
			LastTransitionTime: metaapi.Now(),
			Reason:             condstate.Reason,
			Message:            condstate.Message,
		})
	}

	cr.Status.Conditions = conditions
}

func isDeploymentStatusAvailable(deploy *appsapi.Deployment) bool {
	return deploy.Status.AvailableReplicas > 0
}

func isDeploymentStatusComplete(deploy *appsapi.Deployment) bool {
	replicas := int32(1)
	if deploy.Spec.Replicas != nil {
		replicas = *(deploy.Spec.Replicas)
	}
	return deploy.Status.UpdatedReplicas == replicas &&
		deploy.Status.Replicas == replicas &&
		deploy.Status.AvailableReplicas == replicas &&
		deploy.Status.ObservedGeneration >= deploy.Generation
}

func (c *Controller) setStatusRemoving(cr *imageregistryv1.Config) {
	operatorProgressing := operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionTrue,
		Message: "The registry is being removed",
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

func (c *Controller) syncStatus(cr *imageregistryv1.Config, deploy *appsapi.Deployment, applyError error, removed bool) {
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
		} else if removed {
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
	} else if removed {
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
	if removed {
		operatorRemoved.Status = operatorapiv1.ConditionTrue
		operatorRemoved.Message = "The registry is removed"
		operatorRemoved.Reason = "Removed"
	}

	updateCondition(cr, defaults.OperatorStatusTypeRemoved, operatorRemoved)
}
