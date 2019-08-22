package operator

import (
	"fmt"

	"k8s.io/klog"

	appsapi "k8s.io/api/apps/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapiv1 "github.com/openshift/api/operator/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
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

// updateCoreStatusConditions updates the core operator conditions based on the state of the deployment and parameters passed in.
// The following conditions are set:
//   1. Available
//   2. Progressing
//   3. Degraded
//   4. Removed
// Conditions other than the four listed above should be set prior to calling updateCoreStatusConditions.
func (c *Controller) updateCoreStatusConditions(cr *imageregistryv1.Config, deploy *appsapi.Deployment, applyError error, removed bool, hasTrustedCa bool) {
	operatorAvailable := operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionFalse,
		Message: "",
		Reason:  "",
	}
	degradedTimeoutReached := c.degradedTimeoutExceeded(cr, imageregistryv1.HasTrustedCA)
	if deploy == nil {
		if e, ok := applyError.(permanentError); ok {
			operatorAvailable.Message = applyError.Error()
			operatorAvailable.Reason = e.Reason
		} else {
			operatorAvailable.Message = "The deployment does not exist"
			operatorAvailable.Reason = "DeploymentNotFound"
		}
	} else if deploy.DeletionTimestamp != nil {
		operatorAvailable.Message = "The deployment is being deleted"
		operatorAvailable.Reason = "DeploymentDeleted"
	} else if !hasTrustedCa {
		// 4.2 - on upgrade, the trusted CA bundle may not be present, and the networking operator may
		// take a long time to inject the CA bundle.
		// In this situation the operator should initially report itself at level, but report degraded after a reasonable timeout.
		// Later, when the trusted CA bundle is present, we proceed as usual and upgrade the operands.
		operatorAvailable.Message = fmt.Sprintf("Trusted CA bundle is not present after waiting %s.", c.degradedTimeout)
		operatorAvailable.Reason = "NoTrustedCA"
		if !degradedTimeoutReached {
			operatorAvailable.Message = "TrustedCA bundle is not present."
			operatorAvailable.Status = operatorapiv1.ConditionTrue
		} else {
			klog.Error(operatorAvailable.Message)
		}
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
	} else if !hasTrustedCa {
		operatorProgressing.Status = operatorapiv1.ConditionFalse
		operatorProgressing.Message = "Trusted CA bundle is not present."
		operatorProgressing.Reason = "NoTrustedCA"
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
	if e, ok := applyError.(permanentError); ok {
		operatorDegraded.Status = operatorapiv1.ConditionTrue
		operatorDegraded.Message = applyError.Error()
		operatorDegraded.Reason = e.Reason
	} else if !hasTrustedCa {
		// 4.2 - on upgrade, the trusted CA bundle may not be present, and the networking operator may
		// take a long time to inject the CA bundle.
		// In this situation the operator should initially report itself at level, but report degraded after a reasonable timeout.
		// Later, when the trusted CA bundle is present, we proceed as usual and upgrade the operands.
		operatorDegraded.Message = "Trusted CA bundle is not present."
		operatorDegraded.Reason = "NoTrustedCA"
		if degradedTimeoutReached {
			operatorDegraded.Status = operatorapiv1.ConditionTrue
			operatorDegraded.Message = fmt.Sprintf("Trusted CA bundle is not present after waiting %s.", c.degradedTimeout)
		}
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
	}

	updateCondition(cr, imageregistryv1.OperatorStatusTypeRemoved, operatorRemoved)
}

// setTrustedCAStatus reports if the trusted CA bundle has been injected into the registry and registry operator.
func (c *Controller) setTrustedCAStatus(cr *imageregistryv1.Config, hasTrustedCA bool) {
	operatorHasTrust := operatorapiv1.OperatorCondition{
		Status:  operatorapiv1.ConditionFalse,
		Reason:  "NoTrustedCA",
		Message: "Trusted CA bundle is not present.",
	}
	if hasTrustedCA {
		operatorHasTrust.Status = operatorapiv1.ConditionTrue
		operatorHasTrust.Reason = "Available"
		operatorHasTrust.Message = "Trusted CA bundle is present."
	}
	updateCondition(cr, imageregistryv1.HasTrustedCA, operatorHasTrust)
}
