package operator

import (
	"fmt"

	"github.com/golang/glog"

	appsapi "k8s.io/api/apps/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"

	configapiv1 "github.com/openshift/api/config/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusteroperator"
)

func updateCondition(cr *imageregistryv1.Config, condtype string, condstate clusteroperator.ConditionState) {
	found := false
	conditions := []operatorapiv1.OperatorCondition{}

	for _, c := range cr.Status.Conditions {
		if c.Type != condtype {
			conditions = append(conditions, c)
			continue
		}
		if c.Status != operatorapiv1.ConditionStatus(condstate.Status) {
			c.Status = operatorapiv1.ConditionStatus(condstate.Status)
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

// isDeploymentStatusAvailableAndUpdated returns true when at least one
// replica instance exists and all replica instances are current,
// there are no replica instances remaining from the previous deployment.
// There may still be additional replica instances being created.
func isDeploymentStatusAvailableAndUpdated(deploy *appsapi.Deployment) bool {
	return deploy.Status.AvailableReplicas > 0 &&
		deploy.Status.ObservedGeneration >= deploy.Generation &&
		deploy.Status.UpdatedReplicas == deploy.Status.Replicas
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
	operatorProgressing := clusteroperator.ConditionState{
		Status:  configapiv1.ConditionTrue,
		Message: "The registry is being removed",
		Reason:  "Removing",
	}

	err := c.clusterStatus.Update(configapiv1.OperatorProgressing, operatorProgressing, "")
	if err != nil {
		glog.Errorf("unable to update cluster status to %s=%s: %s", configapiv1.OperatorProgressing, configapiv1.ConditionTrue, err)
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeProgressing, operatorProgressing)
}

func (c *Controller) setStatusRemoveFailed(cr *imageregistryv1.Config, removeErr error) {
	operatorFailing := clusteroperator.ConditionState{
		Status:  configapiv1.ConditionTrue,
		Message: fmt.Sprintf("Unable to remove registry: %s", removeErr),
		Reason:  "RemoveFailed",
	}

	err := c.clusterStatus.Update(configapiv1.OperatorDegraded, operatorFailing, "")
	if err != nil {
		glog.Errorf("unable to update cluster status to %s=%s: %s", configapiv1.OperatorDegraded, configapiv1.ConditionTrue, err)
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeDegraded, operatorFailing)
}

func (c *Controller) syncStatus(cr *imageregistryv1.Config, deploy *appsapi.Deployment, applyError error, removed bool) {
	setOperatorVersion := false
	operatorAvailable := clusteroperator.ConditionState{
		Status:  configapiv1.ConditionFalse,
		Message: "",
		Reason:  "",
	}
	if deploy == nil {
		operatorAvailable.Message = "The deployment does not exist"
		operatorAvailable.Reason = "DeploymentNotFound"
	} else if deploy.DeletionTimestamp != nil {
		operatorAvailable.Message = "The deployment is being deleted"
		operatorAvailable.Reason = "DeploymentDeleted"
	} else if !isDeploymentStatusAvailable(deploy) {
		operatorAvailable.Message = "The deployment does not have available replicas"
		operatorAvailable.Reason = "NoReplicasAvailable"
	} else if !isDeploymentStatusComplete(deploy) {
		operatorAvailable.Status = configapiv1.ConditionTrue
		operatorAvailable.Message = "The registry has minimum availability"
		operatorAvailable.Reason = "MinimumAvailability"
		setOperatorVersion = isDeploymentStatusAvailableAndUpdated(deploy)
	} else {
		operatorAvailable.Status = configapiv1.ConditionTrue
		operatorAvailable.Message = "The registry is ready"
		operatorAvailable.Reason = "Ready"
		setOperatorVersion = true
	}

	deploymentVersion := ""
	// if the current deployment has achieved availability at the new level, set the operator reported
	// version to whatever level the deployment has achieved.
	if setOperatorVersion {
		deploymentVersion = deploy.Annotations[imageregistryv1.VersionAnnotation]
	}
	err := c.clusterStatus.Update(configapiv1.OperatorAvailable, operatorAvailable, deploymentVersion)
	if err != nil {
		glog.Errorf("unable to update cluster status to %s=%s (%s): %s", configapiv1.OperatorAvailable, operatorAvailable.Status, operatorAvailable.Message, err)
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeAvailable, operatorAvailable)

	operatorProgressing := clusteroperator.ConditionState{
		Status:  configapiv1.ConditionTrue,
		Message: "",
		Reason:  "",
	}
	if cr.Spec.ManagementState == operatorapiv1.Unmanaged {
		operatorProgressing.Status = configapiv1.ConditionFalse
		operatorProgressing.Message = "The registry configuration is set to unmanaged mode"
		operatorProgressing.Reason = "Unmanaged"
	} else if removed {
		if deploy != nil {
			operatorProgressing.Message = "The deployment is being removed"
			operatorProgressing.Reason = "DeletingDeployment"
		} else {
			operatorProgressing.Status = configapiv1.ConditionFalse
			operatorProgressing.Message = "All registry resources are removed"
			operatorProgressing.Reason = "Removed"
		}
	} else if applyError != nil {
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
		operatorProgressing.Status = configapiv1.ConditionFalse
		operatorProgressing.Message = "The registry is ready"
		operatorProgressing.Reason = "Ready"
	}

	err = c.clusterStatus.Update(configapiv1.OperatorProgressing, operatorProgressing, "")
	if err != nil {
		glog.Errorf("unable to update cluster status to %s=%s (%s): %s", configapiv1.OperatorProgressing, operatorProgressing.Status, operatorProgressing.Message, err)
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeProgressing, operatorProgressing)

	operatorFailing := clusteroperator.ConditionState{
		Status:  configapiv1.ConditionFalse,
		Message: "",
		Reason:  "",
	}
	if e, ok := applyError.(permanentError); ok {
		operatorFailing.Status = configapiv1.ConditionTrue
		operatorFailing.Message = applyError.Error()
		operatorFailing.Reason = e.Reason
	}

	err = c.clusterStatus.Update(configapiv1.OperatorDegraded, operatorFailing, "")
	if err != nil {
		glog.Errorf("unable to update cluster status to %s=%s (%s): %s", configapiv1.OperatorDegraded, operatorFailing.Status, operatorFailing.Message, err)
	}

	updateCondition(cr, operatorapiv1.OperatorStatusTypeDegraded, operatorFailing)

	operatorRemoved := clusteroperator.ConditionState{
		Status:  configapiv1.ConditionFalse,
		Message: "",
		Reason:  "",
	}
	if removed {
		operatorRemoved.Status = configapiv1.ConditionTrue
		operatorRemoved.Message = "The registry is removed"
	}

	updateCondition(cr, imageregistryv1.OperatorStatusTypeRemoved, operatorRemoved)
}
