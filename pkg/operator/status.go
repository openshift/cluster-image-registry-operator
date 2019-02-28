package operator

import (
	"fmt"

	"github.com/golang/glog"

	appsapi "k8s.io/api/apps/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapi "github.com/openshift/api/operator/v1"

	osapi "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
)

func updateCondition(cr *imageregistryv1.Config, condition *operatorapi.OperatorCondition) {
	found := false
	conditions := []operatorapi.OperatorCondition{}

	for _, c := range cr.Status.Conditions {
		if condition.Type != c.Type {
			conditions = append(conditions, c)
			continue
		}
		if c.Status != condition.Status {
			c.Status = condition.Status
			c.LastTransitionTime = condition.LastTransitionTime
		}
		if c.Reason != condition.Reason {
			c.Reason = condition.Reason
		}
		if c.Message != condition.Message {
			c.Message = condition.Message
		}
		conditions = append(conditions, c)
		found = true
	}

	if !found {
		conditions = append(conditions, *condition)
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

func (c *Controller) syncStatus(cr *imageregistryv1.Config, deploy *appsapi.Deployment, applyError error, removed bool) {
	setOperatorVersion := false
	operatorAvailable := osapi.ConditionFalse
	operatorAvailableMsg := ""
	if deploy == nil {
		operatorAvailableMsg = "Deployment does not exist"
	} else if deploy.DeletionTimestamp != nil {
		operatorAvailableMsg = "Deployment is being deleted"
	} else if !isDeploymentStatusAvailable(deploy) {
		operatorAvailableMsg = "Deployment does not have available replicas"
	} else {
		operatorAvailable = osapi.ConditionTrue
		operatorAvailableMsg = "Deployment has minimum availability"
		setOperatorVersion = isDeploymentStatusAvailableAndUpdated(deploy)
	}

	deploymentVersion := ""
	// if the current deployment has achieved availability at the new level, set the operator reported
	// version to whatever level the deployment has achieved.
	if setOperatorVersion {
		deploymentVersion = deploy.Annotations[imageregistryv1.VersionAnnotation]
	}
	err := c.clusterStatus.Update(osapi.OperatorAvailable, operatorAvailable, operatorAvailableMsg, deploymentVersion)
	if err != nil {
		glog.Errorf("unable to update cluster status to %s=%s (%s): %s", osapi.OperatorAvailable, operatorAvailable, operatorAvailableMsg, err)
	}

	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeAvailable,
		Status:             operatorapi.ConditionStatus(operatorAvailable),
		LastTransitionTime: metaapi.Now(),
		Message:            operatorAvailableMsg,
	})

	operatorProgressing := osapi.ConditionTrue
	operatorProgressingMsg := ""
	if cr.Spec.ManagementState == operatorapi.Unmanaged {
		operatorProgressing = osapi.ConditionFalse
		operatorProgressingMsg = "Unmanaged"
	} else if removed {
		if deploy != nil {
			operatorProgressingMsg = "The deployment still exists"
		} else {
			operatorProgressing = osapi.ConditionFalse
			operatorProgressingMsg = "Everything is removed"
		}
	} else if applyError != nil {
		operatorProgressingMsg = fmt.Sprintf("Unable to apply resources: %s", applyError)
	} else if deploy == nil {
		operatorProgressingMsg = "All resources are successfully applied, but the deployment does not exist"
	} else if deploy.DeletionTimestamp != nil {
		operatorProgressingMsg = "The deployment is being deleted"
	} else if !isDeploymentStatusComplete(deploy) {
		operatorProgressingMsg = "The deployment has not completed"
	} else {
		operatorProgressing = osapi.ConditionFalse
		operatorProgressingMsg = "Everything is ready"
	}

	err = c.clusterStatus.Update(osapi.OperatorProgressing, operatorProgressing, operatorProgressingMsg, "")
	if err != nil {
		glog.Errorf("unable to update cluster status to %s=%s (%s): %s", osapi.OperatorProgressing, operatorProgressing, operatorProgressingMsg, err)
	}

	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeProgressing,
		Status:             operatorapi.ConditionStatus(operatorProgressing),
		LastTransitionTime: metaapi.Now(),
		Message:            operatorProgressingMsg,
	})

	operatorFailing := osapi.ConditionFalse
	operatorFailingMsg := ""
	if _, ok := applyError.(permanentError); ok {
		operatorFailing = osapi.ConditionTrue
		operatorFailingMsg = applyError.Error()
	}

	err = c.clusterStatus.Update(osapi.OperatorFailing, operatorFailing, operatorFailingMsg, "")
	if err != nil {
		glog.Errorf("unable to update cluster status to %s=%s (%s): %s", osapi.OperatorFailing, operatorFailing, operatorFailingMsg, err)
	}

	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeFailing,
		Status:             operatorapi.ConditionStatus(operatorFailing),
		LastTransitionTime: metaapi.Now(),
		Message:            operatorFailingMsg,
	})

	operatorRemoved := osapi.ConditionFalse
	operatorRemovedMsg := ""
	if removed {
		operatorRemoved = osapi.ConditionTrue
		operatorRemovedMsg = "The image registry is removed"
	}

	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               imageregistryv1.OperatorStatusTypeRemoved,
		Status:             operatorapi.ConditionStatus(operatorRemoved),
		LastTransitionTime: metaapi.Now(),
		Message:            operatorRemovedMsg,
	})
}
