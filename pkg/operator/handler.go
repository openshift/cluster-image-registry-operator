package operator

import (
	"context"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/operator-framework/operator-sdk/pkg/util/k8sutil"

	kappsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacapi "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsapi "github.com/openshift/api/apps/v1"
	operatorapi "github.com/openshift/api/operator/v1alpha1"
	routeapi "github.com/openshift/api/route/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	osapi "github.com/openshift/cluster-version-operator/pkg/apis/operatorstatus.openshift.io/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/generate"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

func NewHandler(namespace string) (sdk.Handler, error) {
	operatorNamespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		logrus.Fatalf("Failed to get watch namespace: %v", err)
	}

	operatorName, err := k8sutil.GetOperatorName()
	if err != nil {
		logrus.Fatalf("Failed to get operator name: %v", err)
	}

	p := parameters.Globals{}

	p.Deployment.Namespace = namespace
	p.Deployment.Labels = map[string]string{"docker-registry": "default"}

	p.Pod.ServiceAccount = "registry"
	p.Container.Port = 5000

	p.Healthz.Route = "/healthz"
	p.Healthz.TimeoutSeconds = 5

	p.Service.Name = "image-registry"

	h := &Handler{
		name:               operatorName,
		namespace:          operatorNamespace,
		params:             p,
		generateDeployment: generate.Deployment,
	}

	_, err = h.Bootstrap()
	if err != nil {
		return nil, err
	}

	err = h.createClusterOperator()
	if err != nil {
		logrus.Errorf("unable to create cluster operator resource: %s", err)
	}

	return h, nil
}

type Handler struct {
	name               string
	namespace          string
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

func (h *Handler) reCreateByEvent(event sdk.Event, gen generate.Generator) (*regopapi.ImageRegistry, bool, error) {
	o := event.Object.(metav1.Object)

	cr, err := h.getImageRegistryForResource(o)
	if err != nil {
		return nil, false, err
	}

	if cr == nil || !metav1.IsControlledBy(o, cr) {
		return cr, false, nil
	}

	statusChanged := false

	tmpl, err := gen(cr, &h.params)
	if err != nil {
		conditionResourceApply(cr, operatorapi.ConditionFalse,
			fmt.Sprintf("unable to make template for %T %s/%s: %s", o, o.GetNamespace(), o.GetName(), err),
			&statusChanged,
		)
		return cr, statusChanged, nil
	}

	err = generate.ApplyTemplate(tmpl, false, &statusChanged)
	if err != nil {
		conditionResourceApply(cr, operatorapi.ConditionFalse,
			fmt.Sprintf("unable to apply template %s: %s", tmpl.Name(), err),
			&statusChanged,
		)
		return cr, statusChanged, nil
	}

	if statusChanged {
		logrus.Debugf("resource %s is recreated", tmpl.Name())
		conditionResourceApply(cr, operatorapi.ConditionTrue, "all resources applied", &statusChanged)
	}

	return cr, statusChanged, nil
}

func (h *Handler) reDeployByEvent(event sdk.Event, gen generate.Generator) (*regopapi.ImageRegistry, bool, error) {
	cr, statusChanged, err := h.reCreateByEvent(event, gen)
	if err != nil {
		return cr, statusChanged, err
	}

	if !statusChanged {
		return cr, false, nil
	}

	tmpl, err := h.generateDeployment(cr, &h.params)
	if err != nil {
		conditionResourceApply(cr, operatorapi.ConditionFalse,
			fmt.Sprintf("unable to make template for %T: %s", event.Object, err),
			&statusChanged,
		)
		return cr, statusChanged, nil
	}

	err = generate.ApplyTemplate(tmpl, true, &statusChanged)
	if err != nil {
		conditionResourceApply(cr, operatorapi.ConditionFalse,
			fmt.Sprintf("unable to apply template %s: %s", tmpl.Name(), err),
			&statusChanged,
		)
		return cr, statusChanged, nil
	}

	conditionResourceApply(cr, operatorapi.ConditionTrue, "all resources applied", &statusChanged)

	return cr, statusChanged, nil
}

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	var (
		statusChanged bool
		err           error
		cr            *regopapi.ImageRegistry
	)

	switch o := event.Object.(type) {
	case *rbacapi.ClusterRole:
		cr, statusChanged, err = h.reCreateByEvent(event, generate.ClusterRole)
		if err != nil {
			return err
		}

	case *rbacapi.ClusterRoleBinding:
		cr, statusChanged, err = h.reCreateByEvent(event, generate.ClusterRoleBinding)
		if err != nil {
			return err
		}

	case *corev1.Service:
		cr, statusChanged, err = h.reCreateByEvent(event, generate.Service)
		if err != nil {
			return err
		}

	case *corev1.ServiceAccount:
		cr, statusChanged, err = h.reDeployByEvent(event, generate.ServiceAccount)
		if err != nil {
			return err
		}

	case *corev1.ConfigMap:
		cr, statusChanged, err = h.reDeployByEvent(event, generate.ConfigMap)
		if err != nil {
			return err
		}

	case *corev1.Secret:
		cr, statusChanged, err = h.reDeployByEvent(event, generate.Secret)
		if err != nil {
			return err
		}

	case *routeapi.Route:
		cr, err = h.getImageRegistryForResource(&o.ObjectMeta)
		if err != nil {
			return err
		}

		if cr == nil || !metav1.IsControlledBy(o, cr) {
			return nil
		}

		routes := generate.GetRouteGenerators(cr, &h.params)

		if gen, found := routes[o.ObjectMeta.Name]; found {
			tmpl, err := gen(cr, &h.params)
			if err != nil {
				conditionResourceApply(cr, operatorapi.ConditionFalse,
					fmt.Sprintf("unable to make template for %T %s/%s: %s", o, o.GetNamespace(), o.GetName(), err),
					&statusChanged,
				)
				break
			}

			err = generate.ApplyTemplate(tmpl, false, &statusChanged)
			if err != nil {
				conditionResourceApply(cr, operatorapi.ConditionFalse,
					fmt.Sprintf("unable to apply template %s: %s", tmpl.Name(), err),
					&statusChanged,
				)
				break
			}
		}

	case *kappsapi.Deployment:
		cr, err = h.getImageRegistryForResource(o)
		if err != nil {
			return err
		}

		if cr == nil || !metav1.IsControlledBy(o, cr) {
			return nil
		}

		if event.Deleted {
			tmpl, err := generate.Deployment(cr, &h.params)
			if err != nil {
				return err
			}

			err = generate.ApplyTemplate(tmpl, false, &statusChanged)
			if err != nil {
				return err
			}

			logrus.Debugf("resource %s is recreated", tmpl.Name())
			break
		}

		if o.Status.ReadyReplicas > 0 {
			errOp := h.operatorStatus(osapi.OperatorAvailable, osapi.ConditionTrue, "")
			if errOp != nil {
				logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorAvailable, osapi.ConditionTrue, errOp)
			}
		}

		if cr.Spec.Replicas == o.Status.ReadyReplicas {
			errOp := h.operatorStatus(osapi.OperatorProgressing, osapi.ConditionFalse, "")
			if errOp != nil {
				logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorProgressing, osapi.ConditionFalse, errOp)
			}
			conditionDeployment(cr, operatorapi.ConditionTrue, "deployment successfully progressed", &statusChanged)
		} else {
			conditionDeployment(cr, operatorapi.ConditionFalse, "not enough replicas", &statusChanged)
		}

	case *appsapi.DeploymentConfig:
		cr, err = h.getImageRegistryForResource(&o.ObjectMeta)
		if err != nil {
			return err
		}

		if cr == nil || !metav1.IsControlledBy(o, cr) {
			return nil
		}

		if event.Deleted {
			tmpl, err := generate.DeploymentConfig(cr, &h.params)
			if err != nil {
				return err
			}

			err = generate.ApplyTemplate(tmpl, false, &statusChanged)
			if err != nil {
				return err
			}

			logrus.Debugf("resource %s is recreated", tmpl.Name())
			break
		}

		if o.Status.ReadyReplicas > 0 {
			errOp := h.operatorStatus(osapi.OperatorAvailable, osapi.ConditionTrue, "")
			if errOp != nil {
				logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorAvailable, osapi.ConditionTrue, errOp)
			}
		}

		if cr.Spec.Replicas == o.Status.ReadyReplicas {
			errOp := h.operatorStatus(osapi.OperatorProgressing, osapi.ConditionFalse, "")
			if errOp != nil {
				logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorProgressing, osapi.ConditionFalse, errOp)
			}
			conditionDeployment(cr, operatorapi.ConditionTrue, "deployment successfully progressed", &statusChanged)
		} else {
			conditionDeployment(cr, operatorapi.ConditionFalse, "not enough replicas", &statusChanged)
		}

	case *regopapi.ImageRegistry:
		cr = event.Object.(*regopapi.ImageRegistry)

		if cr.ObjectMeta.DeletionTimestamp != nil {
			cr, err = h.Bootstrap()
			if err != nil {
				return err
			}
		}

		switch cr.Spec.ManagementState {
		case operatorapi.Removed:
			err = h.RemoveResources(cr)
			if err != nil {
				conditionResourceApply(o, operatorapi.ConditionFalse, fmt.Sprintf("unable to remove objects: %s", err), &statusChanged)
			}
		case operatorapi.Managed:
			errOp := h.operatorStatus(osapi.OperatorProgressing, osapi.ConditionTrue, "registry server is in the process of deployment")
			if errOp != nil {
				logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorProgressing, osapi.ConditionFalse, errOp)
			}

			err = h.CreateOrUpdateResources(cr, &statusChanged)

			if err != nil {
				errOp := h.operatorStatus(osapi.OperatorFailing, osapi.ConditionTrue, "unable to deploy registry")
				if errOp != nil {
					logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorFailing, osapi.ConditionTrue, errOp)
				}
				conditionResourceApply(o, operatorapi.ConditionFalse, err.Error(), &statusChanged)
			} else {
				errOp := h.operatorStatus(osapi.OperatorFailing, osapi.ConditionFalse, "")
				if errOp != nil {
					logrus.Errorf("unable to update cluster status to %s=%s: %s", osapi.OperatorFailing, osapi.ConditionFalse, errOp)
				}
				conditionResourceApply(o, operatorapi.ConditionTrue, "all resources applied", &statusChanged)
			}
		case operatorapi.Unmanaged:
			// ignore
		}
	}

	if cr != nil && statusChanged {
		logrus.Infof("%s %s/%s changed", cr.GetObjectKind().GroupVersionKind().Kind, cr.Namespace, cr.Name)

		cr.Status.ObservedGeneration = cr.Generation

		err = sdk.Update(cr)
		if err != nil && !errors.IsConflict(err) {
			logrus.Errorf("unable to update %s %s/%s: %s", cr.GetObjectKind().GroupVersionKind().Kind, cr.Namespace, cr.Name, err)
		}
	}

	return nil
}

func (h *Handler) getImageRegistryForResource(o metav1.Object) (*regopapi.ImageRegistry, error) {
	ownerRef := metav1.GetControllerOf(o)

	if ownerRef == nil || ownerRef.Kind != "ImageRegistry" || ownerRef.APIVersion != regopapi.SchemeGroupVersion.String() {
		return nil, nil
	}

	namespace := o.GetNamespace()
	if len(namespace) == 0 {
		// FIXME
		namespace = os.Getenv("WATCH_NAMESPACE")
	}

	cr := &regopapi.ImageRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: ownerRef.APIVersion,
			Kind:       ownerRef.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ownerRef.Name,
			Namespace: namespace,
		},
	}

	err := sdk.Get(cr)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get %q custom resource: %s", ownerRef.Name, err)
		}
		return nil, nil
	}

	if cr.Spec.ManagementState != operatorapi.Managed {
		return nil, nil
	}

	return cr, nil
}

func (h *Handler) GenerateTemplates(o *regopapi.ImageRegistry, p *parameters.Globals) (ret []generate.Template, err error) {
	generators := []generate.Generator{
		generate.ClusterRole,
		generate.ClusterRoleBinding,
		generate.ServiceAccount,
		generate.ConfigMap,
		generate.Secret,
		generate.Service,
	}

	routes := generate.GetRouteGenerators(o, p)
	for i := range routes {
		generators = append(generators, routes[i])
	}

	generators = append(generators, h.generateDeployment)

	ret = make([]generate.Template, len(generators))

	for i, gen := range generators {
		ret[i], err = gen(o, p)
		if err != nil {
			return
		}
	}

	return
}

func (h *Handler) CreateOrUpdateResources(o *regopapi.ImageRegistry, modified *bool) error {
	appendFinalizer(o, modified)

	err := verifyResource(o, &h.params)
	if err != nil {
		return fmt.Errorf("unable to complete resource: %s", err)
	}

	configState, err := generate.GetConfigState(h.params.Deployment.Namespace)
	if err != nil {
		return fmt.Errorf("unable to get previous config state: %s", err)
	}

	driver, err := storage.NewDriver(&o.Spec.Storage)
	if err != nil {
		return fmt.Errorf("unable to create storage driver: %s", err)
	}

	err = driver.ValidateConfiguration(configState)
	if err != nil {
		return fmt.Errorf("bad custom resource: %s", err)
	}

	templates, err := h.GenerateTemplates(o, &h.params)
	if err != nil {
		return fmt.Errorf("unable to genetate templates: %s", err)
	}

	for _, tpl := range templates {
		err = generate.ApplyTemplate(tpl, false, modified)
		if err != nil {
			return fmt.Errorf("unable to apply objects: %s", err)
		}
	}

	err = syncRoutes(o, &h.params, modified)
	if err != nil {
		return fmt.Errorf("unable to sync routes: %s", err)
	}

	err = generate.SetConfigState(o, configState)
	if err != nil {
		return fmt.Errorf("unable to write current config state: %s", err)
	}

	return nil
}
