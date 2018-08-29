package generate

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsapi "github.com/openshift/api/apps/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func DeploymentConfig(cr *v1alpha1.OpenShiftDockerRegistry, p *parameters.Globals) (Template, error) {
	podTemplateSpec, annotations, err := PodTemplateSpec(cr, p)
	if err != nil {
		return Template{}, err
	}

	dc := &appsapi.DeploymentConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsapi.SchemeGroupVersion.String(),
			Kind:       "DeploymentConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        p.Deployment.Name,
			Namespace:   p.Deployment.Namespace,
			Labels:      p.Deployment.Labels,
			Annotations: annotations,
		},
		Spec: appsapi.DeploymentConfigSpec{
			Replicas: cr.Spec.Replicas,
			Selector: p.Deployment.Labels,
			Triggers: []appsapi.DeploymentTriggerPolicy{
				{
					Type: appsapi.DeploymentTriggerOnConfigChange,
				},
			},
			Template: &podTemplateSpec,
		},
	}

	addOwnerRefToObject(dc, asOwner(cr))

	return Template{
		Object:   dc,
		Strategy: strategy.DeploymentConfig{},
	}, nil
}
