package operator

import (
	"github.com/sirupsen/logrus"

	kappsapi "k8s.io/api/apps/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	appsapi "github.com/openshift/api/apps/v1"
	operatorapi "github.com/openshift/api/operator/v1alpha1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	osapi "github.com/openshift/cluster-version-operator/pkg/apis/operatorstatus.openshift.io/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/metautil"
)

func updateCondition(cr *regopapi.ImageRegistry, condition *operatorapi.OperatorCondition) bool {
	modified := false
	found := false
	conditions := []operatorapi.OperatorCondition{}

	for _, c := range cr.Status.Conditions {
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

	cr.Status.Conditions = conditions
	return modified
}

func conditionResourceApply(cr *regopapi.ImageRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	if status == operatorapi.ConditionFalse {
		logrus.Errorf("condition failed on %s: %s", metautil.TypeAndName(cr), m)
	}

	changed := updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeAvailable,
		Status:             status,
		LastTransitionTime: metaapi.Now(),
		Reason:             "ResourceApplied",
		Message:            m,
	})

	if changed {
		*modified = true
	}
}

func conditionSyncDeployment(cr *regopapi.ImageRegistry, syncSuccessful operatorapi.ConditionStatus, m string, modified *bool) {
	reason := "DeploymentProgressed"

	if syncSuccessful == operatorapi.ConditionFalse {
		reason = "DeploymentInProgress"
	}

	changed := updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeSyncSuccessful,
		Status:             syncSuccessful,
		LastTransitionTime: metaapi.Now(),
		Reason:             reason,
		Message:            m,
	})

	if changed {
		*modified = true
	}
}

func conditionRemoved(cr *regopapi.ImageRegistry, state operatorapi.ConditionStatus, m string, modified *bool) {
	changed := updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               regopapi.OperatorStatusTypeRemoved,
		Status:             state,
		LastTransitionTime: metaapi.Now(),
		Reason:             "",
		Message:            m,
	})

	if changed {
		*modified = true
	}
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

func (c *Controller) syncDeploymentStatus(cr *regopapi.ImageRegistry, o runtime.Object, statusChanged *bool) {
	metaObject := o.(metaapi.Object)

	operatorAvailable := osapi.ConditionFalse
	operatorAvailableMsg := ""

	if metaObject.GetDeletionTimestamp() == nil && isDeploymentStatusAvailable(o) {
		operatorAvailable = osapi.ConditionTrue
		operatorAvailableMsg = "deployment has minimum availability"
	}

	errOp := c.clusterStatus.Update(osapi.OperatorAvailable, operatorAvailable, operatorAvailableMsg)
	if errOp != nil {
		logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorAvailable, osapi.ConditionTrue, errOp)
	}

	operatorProgressing := osapi.ConditionTrue
	operatorProgressingMsg := "deployment is progressing"

	if metaObject.GetDeletionTimestamp() == nil {
		if isDeploymentStatusComplete(o) {
			operatorProgressing = osapi.ConditionFalse
			operatorProgressingMsg = "deployment successfully progressed"
		}
	} else {
		operatorProgressing = osapi.ConditionFalse
		operatorProgressingMsg = "deployment removed"
	}

	errOp = c.clusterStatus.Update(osapi.OperatorProgressing, operatorProgressing, operatorProgressingMsg)
	if errOp != nil {
		logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorProgressing, operatorProgressing, errOp)
	}

	syncSuccessful := operatorapi.ConditionFalse

	if operatorProgressing == osapi.ConditionFalse {
		syncSuccessful = operatorapi.ConditionTrue
	}

	conditionSyncDeployment(cr, syncSuccessful, operatorProgressingMsg, statusChanged)
}
