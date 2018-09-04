package operator

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	kappsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsapi "github.com/openshift/api/apps/v1"
	operatorapi "github.com/openshift/api/operator/v1alpha1"
	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"

	"github.com/openshift/cluster-image-registry-operator/pkg/generate"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func NewHandler(namespace string, useLegacy bool) sdk.Handler {
	p := parameters.Globals{}

	p.Deployment.Name = "docker-registry"
	p.Deployment.Namespace = namespace
	p.Deployment.Labels = map[string]string{"docker-registry": "default"}

	p.Pod.ServiceAccount = "registry"

	p.Container.Name = "registry"
	p.Container.Port = 5000
	p.Container.UseTLS = false

	p.Healthz.Route = "/healthz"
	p.Healthz.TimeoutSeconds = 5

	h := &Handler{
		params:             p,
		generateDeployment: generate.DeploymentConfig,
	}

	if useLegacy {
		p.Deployment.Name = "docker-registry"
		p.Deployment.Namespace = "default"
		h.generateDeployment = generate.DeploymentConfig
	}

	return h
}

type Handler struct {
	params             parameters.Globals
	generateDeployment generate.Generator
}

func updateCondition(cr *regopapi.OpenShiftDockerRegistry, condition *operatorapi.OperatorCondition) bool {
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

func conditionResourceValid(cr *regopapi.OpenShiftDockerRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	if status == operatorapi.ConditionFalse {
		logrus.Error(m)
	}

	changed := updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeAvailable,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             "ResourceValid",
		Message:            m,
	})

	if changed {
		*modified = true
	}
}

func conditionResourceApply(cr *regopapi.OpenShiftDockerRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	if status == operatorapi.ConditionFalse {
		logrus.Error(m)
	}

	changed := updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeAvailable,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             "ResourceApplied",
		Message:            m,
	})

	if changed {
		*modified = true
	}
}

func conditionDeployment(cr *regopapi.OpenShiftDockerRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	if status == operatorapi.ConditionFalse {
		logrus.Error(m)
	}

	changed := updateCondition(cr, &operatorapi.OperatorCondition{
		Type:               operatorapi.OperatorStatusTypeSyncSuccessful,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             "Progressing",
		Message:            m,
	})

	if changed {
		*modified = true
	}
}

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	// Ignore the delete event since the garbage collector will clean up all secondary resources for the CR
	// All secondary resources must have the CR set as their OwnerReference for this to be the case
	if event.Deleted {
		return nil
	}

	var (
		statusChanged bool
		err           error
		cr            *regopapi.OpenShiftDockerRegistry
	)

	switch event.Object.(type) {
	case *kappsapi.Deployment:
		cr, err = h.getOpenShiftDockerRegistry()
		if err != nil {
			return err
		}

		o := event.Object.(*kappsapi.Deployment)

		if cr == nil || !metav1.IsControlledBy(o, cr) {
			return nil
		}

		if cr.Spec.Replicas == o.Status.ReadyReplicas {
			conditionDeployment(cr, operatorapi.ConditionTrue, "deployment successfully progressed", &statusChanged)
		} else {
			conditionDeployment(cr, operatorapi.ConditionFalse, "not enough replicas", &statusChanged)
		}

	case *appsapi.DeploymentConfig:
		cr, err = h.getOpenShiftDockerRegistry()
		if err != nil {
			return err
		}

		o := event.Object.(*appsapi.DeploymentConfig)

		if cr == nil || !metav1.IsControlledBy(o, cr) {
			return nil
		}

		if cr.Spec.Replicas == o.Status.ReadyReplicas {
			conditionDeployment(cr, operatorapi.ConditionTrue, "deployment successfully progressed", &statusChanged)
		} else {
			conditionDeployment(cr, operatorapi.ConditionFalse, "not enough replicas", &statusChanged)
		}

	case *corev1.ConfigMap, *corev1.Secret:

		cr, err = h.getOpenShiftDockerRegistry()
		if err != nil {
			return err
		}

		o := event.Object.(metav1.Object)

		if cr == nil || !metav1.IsControlledBy(o, cr) {
			return nil
		}

		statusChanged = h.reRollout(cr)

	case *regopapi.OpenShiftDockerRegistry:
		cr = event.Object.(*regopapi.OpenShiftDockerRegistry)

		if cr.Spec.ManagementState != operatorapi.Managed {
			return nil
		}

		statusChanged = h.applyResource(cr)
	}

	if cr != nil && statusChanged {
		logrus.Infof("registry resources changed")

		cr.Status.ObservedGeneration = cr.Generation

		err = sdk.Update(cr)
		if err != nil {
			logrus.Errorf("unable to update registry custom resource: %s", err)
		}
	}

	return nil
}

func (h *Handler) getOpenShiftDockerRegistry() (*regopapi.OpenShiftDockerRegistry, error) {
	cr := &regopapi.OpenShiftDockerRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: regopapi.SchemeGroupVersion.String(),
			Kind:       "OpenShiftDockerRegistry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry",
			Namespace: h.params.Deployment.Namespace,
		},
	}

	err := sdk.Get(cr)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get image-registry custom resource: %s", err)
		}
		return nil, nil
	}

	if cr.Spec.ManagementState != operatorapi.Managed {
		return nil, nil
	}

	return cr, nil
}

func (h *Handler) reRollout(o *regopapi.OpenShiftDockerRegistry) bool {
	modified := false

	dc, err := h.generateDeployment(o, &h.params)
	if err != nil {
		conditionResourceValid(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to make deployment: %s", err), &modified)
		return true
	}

	err = generate.ApplyTemplate(dc, &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply deployment: %s", err), &modified)
		return true
	}

	conditionResourceApply(o, operatorapi.ConditionTrue, "all resources applied", &modified)

	return modified
}

func (h *Handler) applyResource(o *regopapi.OpenShiftDockerRegistry) bool {
	modified := false

	err := completeResource(o, &modified)
	if err != nil {
		conditionResourceValid(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to complete resource: %s", err), &modified)
		return true
	}

	err = generate.ApplyTemplate(generate.ConfigMap(o, &h.params), &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply config map: %s", err), &modified)
		return true
	}

	err = generate.ApplyTemplate(generate.Secret(o, &h.params), &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply secret: %s", err), &modified)
		return true
	}

	dc, err := h.generateDeployment(o, &h.params)
	if err != nil {
		conditionResourceValid(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to make deployment: %s", err), &modified)
		return true
	}

	err = generate.ApplyTemplate(generate.ServiceAccount(o, &h.params), &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply service account: %s", err), &modified)
		return true
	}

	err = generate.ApplyTemplate(generate.ClusterRole(o), &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply cluster role: %s", err), &modified)
		return true
	}

	err = generate.ApplyTemplate(generate.ClusterRoleBinding(o, &h.params), &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply cluster role binding: %s", err), &modified)
		return true
	}

	err = generate.ApplyTemplate(generate.Service(o, &h.params), &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply service: %s", err), &modified)
		return true
	}

	err = generate.ApplyTemplate(dc, &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply deployment: %s", err), &modified)
		return true
	}

	conditionResourceApply(o, operatorapi.ConditionTrue, "all resources applied", &modified)

	return modified
}
