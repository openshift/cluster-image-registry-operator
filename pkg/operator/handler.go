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
	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"

	"github.com/openshift/cluster-image-registry-operator/pkg/generate"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

func NewHandler(namespace string) (sdk.Handler, error) {
	p := parameters.Globals{}

	p.Deployment.Name = "docker-registry"
	p.Deployment.Namespace = namespace
	p.Deployment.Labels = map[string]string{"docker-registry": "default"}

	p.Pod.ServiceAccount = "registry"

	p.Container.Name = "registry"
	p.Container.Port = 5000

	p.Healthz.Route = "/healthz"
	p.Healthz.TimeoutSeconds = 5

	p.DefaultRoute.Name = "image-registry-default-route"

	h := &Handler{
		params:             p,
		generateDeployment: generate.DeploymentConfig,
	}

	err := h.bootstrap()
	if err != nil {
		return nil, err
	}

	return h, nil
}

type Handler struct {
	params             parameters.Globals
	generateDeployment generate.Generator
}

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

func conditionResourceValid(cr *regopapi.ImageRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	if status == operatorapi.ConditionFalse {
		logrus.Errorf("condition failed on %s %s/%s: %s", cr.GetObjectKind().GroupVersionKind().Kind, cr.Namespace, cr.Name, m)
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

func conditionResourceApply(cr *regopapi.ImageRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	if status == operatorapi.ConditionFalse {
		logrus.Errorf("condition failed on %s %s/%s: %s", cr.GetObjectKind().GroupVersionKind().Kind, cr.Namespace, cr.Name, m)
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

func conditionDeployment(cr *regopapi.ImageRegistry, status operatorapi.ConditionStatus, m string, modified *bool) {
	if status == operatorapi.ConditionFalse {
		logrus.Errorf("condition failed on %s %s/%s: %s", cr.GetObjectKind().GroupVersionKind().Kind, cr.Namespace, cr.Name, m)
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
		cr            *regopapi.ImageRegistry
	)

	switch event.Object.(type) {
	case *kappsapi.Deployment:
		cr, err = h.getImageRegistry()
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
		cr, err = h.getImageRegistry()
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

		cr, err = h.getImageRegistry()
		if err != nil {
			return err
		}

		o := event.Object.(metav1.Object)

		if cr == nil || !metav1.IsControlledBy(o, cr) {
			return nil
		}

		statusChanged = h.reRollout(cr)

	case *regopapi.ImageRegistry:
		cr = event.Object.(*regopapi.ImageRegistry)

		if cr.Spec.ManagementState != operatorapi.Managed {
			return nil
		}

		statusChanged = h.ResyncResources(cr)
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

func (h *Handler) getImageRegistry() (*regopapi.ImageRegistry, error) {
	cr := &regopapi.ImageRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: regopapi.SchemeGroupVersion.String(),
			Kind:       "ImageRegistry",
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

func (h *Handler) reRollout(o *regopapi.ImageRegistry) bool {
	modified := false

	dc, err := h.generateDeployment(o, &h.params)
	if err != nil {
		conditionResourceValid(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to make deployment: %s", err), &modified)
		return true
	}

	err = generate.ApplyTemplate(dc, true, &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply deployment: %s", err), &modified)
		return true
	}

	conditionResourceApply(o, operatorapi.ConditionTrue, "all resources applied", &modified)

	return modified
}

func (h *Handler) GenerateTemplates(o *regopapi.ImageRegistry, p *parameters.Globals) ([]generate.Template, error) {
	var ret []generate.Template

	ret = append(ret, generate.ConfigMap(o, p))
	ret = append(ret, generate.Secret(o, p))
	ret = append(ret, generate.ServiceAccount(o, p))
	ret = append(ret, generate.ClusterRole(o))
	ret = append(ret, generate.ClusterRoleBinding(o, p))
	ret = append(ret, generate.Service(o, p))

	if o.Spec.DefaultRoute {
		ret = append(ret, generate.DefaultRoute(o, p))
	}

	for _, routeSpec := range o.Spec.Routes {
		route, err := generate.Route(o, &routeSpec, p)
		if err != nil {
			return nil, err
		}
		ret = append(ret, route)
	}

	dc, err := h.generateDeployment(o, p)
	if err != nil {
		return nil, err
	}
	ret = append(ret, dc)

	return ret, nil
}

func (h *Handler) ResyncResources(o *regopapi.ImageRegistry) bool {
	modified := false

	err := verifyResource(o, &h.params)
	if err != nil {
		conditionResourceValid(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to complete resource: %s", err), &modified)
		return true
	}

	configState, err := generate.GetConfigState(h.params.Deployment.Namespace)
	if err != nil {
		conditionResourceValid(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to get previous config state: %s", err), &modified)
		return true
	}

	driver, err := storage.NewDriver(&o.Spec.Storage)
	if err != nil {
		conditionResourceValid(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to create storage driver: %s", err), &modified)
		return true
	}

	err = driver.ValidateConfiguration(configState)
	if err != nil {
		conditionResourceValid(o, operatorapi.ConditionFalse, fmt.Sprintf("bad custom resource: %s", err), &modified)
		return true
	}

	templetes, err := h.GenerateTemplates(o, &h.params)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to genetate templates: %s", err), &modified)
		return true
	}

	for _, tpl := range templetes {
		err = generate.ApplyTemplate(tpl, false, &modified)
		if err != nil {
			conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to apply objects: %s", err), &modified)
			return true
		}
	}

	err = syncRoutes(o, &h.params, &modified)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to sync routes: %s", err), &modified)
		return true
	}

	err = generate.SetConfigState(o, configState)
	if err != nil {
		conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to write current config state: %s", err), &modified)
		return true
	}

	conditionResourceApply(o, operatorapi.ConditionTrue, "all resources applied", &modified)

	return modified
}
