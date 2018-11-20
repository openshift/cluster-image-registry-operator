package resource

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

func (g *Generator) makeService(cr *v1alpha1.ImageRegistry) (Template, error) {
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.params.Service.Name,
			Namespace: g.params.Deployment.Namespace,
			Labels:    g.params.Deployment.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: g.params.Deployment.Labels,
			Ports: []corev1.ServicePort{
				{
					Name:       fmt.Sprintf("%d-tcp", g.params.Container.Port),
					Port:       int32(g.params.Container.Port),
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(g.params.Container.Port),
				},
			},
		},
	}

	if cr.Spec.TLS {
		svc.ObjectMeta.Annotations = map[string]string{
			"service.alpha.openshift.io/serving-cert-secret-name": cr.ObjectMeta.Name + "-tls",
		}
	}

	addOwnerRefToObject(svc, asOwner(cr))
	return Template{
		Object:   svc,
		Strategy: strategy.Service{},
	}, nil
}
