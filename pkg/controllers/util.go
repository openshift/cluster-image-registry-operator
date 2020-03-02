package controllers

import (
	"fmt"

	appsapi "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

type ByCreationTimestamp []*batchv1.Job

func (b ByCreationTimestamp) Len() int {
	return len(b)
}

func (b ByCreationTimestamp) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func (b ByCreationTimestamp) Less(i, j int) bool {
	return !b[j].CreationTimestamp.Time.After(b[i].CreationTimestamp.Time)
}

type permanentError struct {
	Err    error
	Reason string
}

func newPermanentError(reason string, err error) error {
	return permanentError{
		Err:    err,
		Reason: reason,
	}
}

func (e permanentError) Error() string {
	return e.Err.Error()
}

func utilObjectInfo(o interface{}) string {
	object := o.(metav1.Object)
	s := fmt.Sprintf("%T, ", o)
	if namespace := object.GetNamespace(); namespace != "" {
		s += fmt.Sprintf("Namespace=%s, ", namespace)
	}
	s += fmt.Sprintf("Name=%s", object.GetName())
	return s
}

func appendFinalizer(cr *imageregistryv1.Config) {
	for i := range cr.ObjectMeta.Finalizers {
		if cr.ObjectMeta.Finalizers[i] == parameters.ImageRegistryOperatorResourceFinalizer {
			return
		}
	}

	cr.ObjectMeta.Finalizers = append(cr.ObjectMeta.Finalizers, parameters.ImageRegistryOperatorResourceFinalizer)
}

func verifyResource(cr *imageregistryv1.Config) error {
	if cr.Spec.Replicas < 0 {
		return fmt.Errorf("replicas must be greater than or equal to 0")
	}

	names := map[string]struct{}{
		defaults.RouteName: {},
	}

	for _, routeSpec := range cr.Spec.Routes {
		_, found := names[routeSpec.Name]
		if found {
			return fmt.Errorf("duplication of names has been detected in the additional routes")
		}
		names[routeSpec.Name] = struct{}{}
	}

	return nil
}

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

func updatePrunerCondition(cr *imageregistryv1.ImagePruner, condtype string, condstate operatorapiv1.OperatorCondition) {
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
