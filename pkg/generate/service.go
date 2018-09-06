package generate

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/strategy"
)

func Service(cr *v1alpha1.OpenShiftDockerRegistry, p *parameters.Globals) Template {
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Deployment.Name,
			Namespace: p.Deployment.Namespace,
			Labels:    p.Deployment.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: p.Deployment.Labels,
			Ports: []corev1.ServicePort{
				{
					Name:       fmt.Sprintf("%d-tcp", p.Container.Port),
					Port:       int32(p.Container.Port),
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(p.Container.Port),
				},
			},
		},
	}

	if cr.Spec.TLS {
		svc.ObjectMeta.Annotations = map[string]string{
			"service.alpha.openshift.io/serving-cert-secret-name": "image-registry-tls",
		}
	}

	addOwnerRefToObject(svc, asOwner(cr))
	return Template{
		Object:   svc,
		Strategy: strategy.Service{},
	}
}
