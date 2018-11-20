package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsapi "github.com/openshift/api/apps/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) makeDeploymentConfig(cr *v1alpha1.ImageRegistry) (Template, error) {
	podTemplateSpec, annotations, err := g.makePodTemplateSpec(cr)
	if err != nil {
		return Template{}, err
	}

	dc := &appsapi.DeploymentConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsapi.SchemeGroupVersion.String(),
			Kind:       "DeploymentConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.ObjectMeta.Name,
			Namespace:   g.params.Deployment.Namespace,
			Labels:      g.params.Deployment.Labels,
			Annotations: annotations,
		},
		Spec: appsapi.DeploymentConfigSpec{
			Replicas: cr.Spec.Replicas,
			Selector: g.params.Deployment.Labels,
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
		Object:      dc,
		Annotations: dc.ObjectMeta.Annotations,
		Strategy:    strategy.DeploymentConfig{},
	}, nil
}
