package operator

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	appsapi "github.com/openshift/api/apps/v1"
	operatorapi "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Parameters struct {
	Deployment struct {
		Name      string
		Namespace string
		Labels    map[string]string
	}
	Pod struct {
		ServiceAccount string
	}
	Container struct {
		UseTLS bool
		Name   string
		Port   int
	}
	Healthz struct {
		Route          string
		TimeoutSeconds int
	}
}

func NewHandler() sdk.Handler {
	p := Parameters{}

	p.Deployment.Name = "docker-registry"
	p.Deployment.Namespace = ""
	p.Deployment.Labels = map[string]string{"docker-registry": "default"}

	p.Pod.ServiceAccount = "registry"

	p.Container.Name = "registry"
	p.Container.Port = 5000
	p.Container.UseTLS = false

	p.Healthz.Route = "/healthz"
	p.Healthz.TimeoutSeconds = 5

	return &Handler{params: p}
}

type Handler struct {
	params Parameters
}

func conditionResourceValid(cr *v1alpha1.OpenShiftDockerRegistry, status operatorapi.ConditionStatus, m string) {
	cr.Status.Conditions = append(cr.Status.Conditions,
		operatorapi.OperatorCondition{
			Type:               operatorapi.OperatorStatusTypeAvailable,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             "ResourceValidation",
			Message:            m,
		},
	)
}

func conditionResourceApply(cr *v1alpha1.OpenShiftDockerRegistry, status operatorapi.ConditionStatus, m string) {
	cr.Status.Conditions = append(cr.Status.Conditions,
		operatorapi.OperatorCondition{
			Type:               operatorapi.OperatorStatusTypeAvailable,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             "ResourceApply",
			Message:            m,
		},
	)
}

func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.OpenShiftDockerRegistry:
		// Ignore the delete event since the garbage collector will clean up all secondary resources for the CR
		// All secondary resources must have the CR set as their OwnerReference for this to be the case
		if event.Deleted {
			return nil
		}

		if o.Spec.ManagementState != operatorapi.Managed {
			return nil
		}

		legacyDC := &appsapi.DeploymentConfig{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps.openshift.io/v1",
				Kind:       "DeploymentConfig",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "docker-registry",
				Namespace: "default",
			},
		}

		err := sdk.Get(legacyDC)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get legacy deployment config: %s", err)
			}
			h.params.Deployment.Namespace = o.Namespace
		} else {
			h.params.Deployment.Name = legacyDC.ObjectMeta.Name
			h.params.Deployment.Namespace = legacyDC.ObjectMeta.Namespace
		}

		statusChanged, err := applyResource(o, &h.params)
		if err != nil {
			return err
		}

		if statusChanged {
			err := sdk.Update(o)
			if err != nil {
				logrus.Errorf("unable to update registry custom resource: %s", err)
			}
		}
	}
	return nil
}

func applyResource(o *v1alpha1.OpenShiftDockerRegistry, p *Parameters) (bool, error) {
	o.Status.Conditions = []operatorapi.OperatorCondition{}

	modified := false

	err := completeResource(o, &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to complete resource: %s", err)

		logrus.Error(msg)
		conditionResourceValid(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	dc, err := GenerateDeploymentConfig(o, p)
	if err != nil {
		msg := fmt.Sprintf("unable to make deployment config: %s", err)

		logrus.Error(msg)
		conditionResourceValid(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	err = ApplyTemplate(GenerateServiceAccount(o, p), &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to apply service account: %s", err)

		logrus.Error(msg)
		conditionResourceApply(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	err = ApplyTemplate(GenerateClusterRole(o), &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to apply cluster role: %s", err)

		logrus.Error(msg)
		conditionResourceApply(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	err = ApplyTemplate(GenerateClusterRoleBinding(o, p), &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to apply cluster role binding: %s", err)

		logrus.Error(msg)
		conditionResourceApply(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	err = ApplyTemplate(GenerateService(o, p), &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to apply service: %s", err)

		logrus.Error(msg)
		conditionResourceApply(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	err = ApplyTemplate(dc, &modified)
	if err != nil {
		msg := fmt.Sprintf("unable to apply deployment config: %s", err)

		logrus.Error(msg)
		conditionResourceApply(o, operatorapi.ConditionFalse, msg)

		return true, nil
	}

	if modified {
		logrus.Infof("registry resources changed")

		conditionResourceApply(o, operatorapi.ConditionTrue, "all resources applied")

		err = sdk.Update(o)
		if err != nil {
			logrus.Errorf("unable to update registry custom resource: %s", err)
			return modified, err
		}
	}

	return modified, nil
}
