package resource

import (
	kappsapi "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) makeDeployment(cr *v1alpha1.ImageRegistry) (Template, error) {
	podTemplateSpec, annotations, err := g.makePodTemplateSpec(cr)
	if err != nil {
		return Template{}, err
	}

	dc := &kappsapi.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kappsapi.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.ObjectMeta.Name,
			Namespace:   g.params.Deployment.Namespace,
			Labels:      g.params.Deployment.Labels,
			Annotations: annotations,
		},
		Spec: kappsapi.DeploymentSpec{
			Replicas: &cr.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: g.params.Deployment.Labels,
			},
			Template: podTemplateSpec,
		},
	}

	addOwnerRefToObject(dc, asOwner(cr))

	return Template{
		Object:      dc,
		Annotations: dc.ObjectMeta.Annotations,
		Strategy:    strategy.Deployment{},
	}, nil
}
