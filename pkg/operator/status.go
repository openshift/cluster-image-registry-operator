package operator

import (
	"fmt"

	kappsapi "k8s.io/api/apps/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"

	appsapi "github.com/openshift/api/apps/v1"
	operatorapi "github.com/openshift/api/operator/v1alpha1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	osapi "github.com/openshift/cluster-version-operator/pkg/apis/operatorstatus.openshift.io/v1"
)

func updateCondition(cr *regopapi.ImageRegistry, condition *operatorapi.OperatorCondition, modified *bool) {
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
			*modified = true
		}
		if c.Reason != condition.Reason {
			c.Reason = condition.Reason
			*modified = true
		}
		if c.Message != condition.Message {
			c.Message = condition.Message
			*modified = true
		}
		conditions = append(conditions, c)
		found = true
	}

	if !found {
		conditions = append(conditions, *condition)
		*modified = true
	}

	cr.Status.Conditions = conditions
}

func conditionRemoved(cr *regopapi.ImageRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               regopapi.OperatorStatusTypeRemoved,
		Status:             status,
		LastTransitionTime: metaapi.Now(),
		Message:            m,
	}, modified)
}

func conditionAvailable(cr *regopapi.ImageRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeAvailable,
		Status:             status,
		LastTransitionTime: metaapi.Now(),
		Message:            m,
	}, modified)
}

func conditionProgressing(cr *regopapi.ImageRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeProgressing,
		Status:             status,
		LastTransitionTime: metaapi.Now(),
		Message:            m,
	}, modified)
}

func conditionFailing(cr *regopapi.ImageRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeFailing,
		Status:             status,
		LastTransitionTime: metaapi.Now(),
		Message:            m,
	}, modified)
}

func isDeploymentStatusAvailable(o runtime.Object) bool {
	switch deploy := o.(type) {
	case *appsapi.DeploymentConfig:
		return deploy.Status.AvailableReplicas > 0
	case *kappsapi.Deployment:
		return deploy.Status.AvailableReplicas > 0
	}
	return false
}

func isDeploymentStatusComplete(o runtime.Object) bool {
	switch deploy := o.(type) {
	case *appsapi.DeploymentConfig:
		return deploy.Status.UpdatedReplicas == deploy.Spec.Replicas &&
			deploy.Status.Replicas == deploy.Spec.Replicas &&
			deploy.Status.AvailableReplicas == deploy.Spec.Replicas &&
			deploy.Status.ObservedGeneration >= deploy.Generation
	case *kappsapi.Deployment:
		replicas := int32(1)
		if deploy.Spec.Replicas != nil {
			replicas = *(deploy.Spec.Replicas)
		}
		return deploy.Status.UpdatedReplicas == replicas &&
			deploy.Status.Replicas == replicas &&
			deploy.Status.AvailableReplicas == replicas &&
			deploy.Status.ObservedGeneration >= deploy.Generation
	}
	return false
}

func (c *Controller) syncStatus(cr *regopapi.ImageRegistry, o runtime.Object, applyError error, statusChanged *bool) {
	metaObject, _ := o.(metaapi.Object)

	operatorAvailable := osapi.ConditionFalse
	operatorAvailableMsg := ""
	if o == nil {
		operatorAvailableMsg = "deployment does not exist"
	} else if metaObject.GetDeletionTimestamp() != nil {
		operatorAvailableMsg = "deployment is being deleted"
	} else if !isDeploymentStatusAvailable(o) {
		operatorAvailableMsg = "deployment does not have available replicas"
	} else {
		operatorAvailable = osapi.ConditionTrue
		operatorAvailableMsg = "deployment has minimum availability"
	}

	err := c.clusterStatus.Update(osapi.OperatorAvailable, operatorAvailable, operatorAvailableMsg)
	if err != nil {
		klog.Errorf("unable to update cluster status to %s=%s (%s): %s", osapi.OperatorAvailable, operatorAvailable, operatorAvailableMsg, err)
	}

	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeAvailable,
		Status:             operatorapi.ConditionStatus(operatorAvailable),
		LastTransitionTime: metaapi.Now(),
		Message:            operatorAvailableMsg,
	}, statusChanged)

	operatorProgressing := osapi.ConditionTrue
	operatorProgressingMsg := ""
	if applyError != nil {
		operatorProgressingMsg = fmt.Sprintf("unable to apply resources: %s", applyError)
	} else if o == nil {
		operatorProgressingMsg = "all resources are successfully applied, but the deployment does not exist"
	} else if metaObject.GetDeletionTimestamp() != nil {
		operatorProgressingMsg = "the deployment is being deleted"
	} else if !isDeploymentStatusComplete(o) {
		operatorProgressingMsg = "the deployment has not completed"
	} else {
		operatorProgressing = osapi.ConditionFalse
		operatorProgressingMsg = "everything is ready"
	}

	err = c.clusterStatus.Update(osapi.OperatorProgressing, operatorProgressing, operatorProgressingMsg)
	if err != nil {
		klog.Errorf("unable to update cluster status to %s=%s (%s): %s", osapi.OperatorProgressing, operatorProgressing, operatorProgressingMsg, err)
	}

	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeProgressing,
		Status:             operatorapi.ConditionStatus(operatorProgressing),
		LastTransitionTime: metaapi.Now(),
		Message:            operatorProgressingMsg,
	}, statusChanged)

	operatorFailing := osapi.ConditionFalse
	operatorFailingMsg := ""
	if _, ok := applyError.(permanentError); ok {
		operatorFailing = osapi.ConditionTrue
		operatorFailingMsg = applyError.Error()
	}

	err = c.clusterStatus.Update(osapi.OperatorFailing, operatorFailing, operatorFailingMsg)
	if err != nil {
		klog.Errorf("unable to update cluster status to %s=%s (%s): %s", osapi.OperatorFailing, operatorFailing, operatorFailingMsg, err)
	}

	updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeFailing,
		Status:             operatorapi.ConditionStatus(operatorFailing),
		LastTransitionTime: metaapi.Now(),
		Message:            operatorFailingMsg,
	}, statusChanged)
}
