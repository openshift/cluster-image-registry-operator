package generate

import (
	"fmt"

	kappsapi "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func Deployment(cr *v1alpha1.OpenShiftDockerRegistry, p *parameters.Globals) (Template, error) {
	podTemplateSpec, annotations, err := PodTemplateSpec(cr, p)
	if err != nil {
		return Template{}, err
	}

	dc := &kappsapi.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kappsapi.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        p.Deployment.Name,
			Namespace:   p.Deployment.Namespace,
			Labels:      p.Deployment.Labels,
			Annotations: annotations,
		},
		Spec: kappsapi.DeploymentSpec{
			Replicas: &cr.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: p.Deployment.Labels,
			},
			Template: podTemplateSpec,
		},
	}

	addOwnerRefToObject(dc, asOwner(cr))

	return Template{
		Object:   dc,
		Strategy: strategy.Deployment{},
		Validator: func(obj runtime.Object) error {
			o, ok := obj.(*kappsapi.Deployment)
			if !ok {
				return fmt.Errorf("bad object: got %T, want *kappsapi.Deployment", obj)
			}
			if annotations != nil && o.ObjectMeta.Annotations != nil {
				curStorage, curOK := annotations[parameters.StorageTypeOperatorAnnotation]
				newStorage, newOK := o.ObjectMeta.Annotations[parameters.StorageTypeOperatorAnnotation]

				if curOK && newOK && curStorage != newStorage {
					return fmt.Errorf("storage type change is not supported: expected storage type %s, but got %s", curStorage, newStorage)
				}
			}
			return nil
		},
	}, nil
}
