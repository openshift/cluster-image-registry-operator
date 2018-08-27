package strategy

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Service struct{}

var _ Strategy = Service{}

func (s Service) Apply(obj, tmpl runtime.Object) (runtime.Object, error) {
	o, ok := obj.(*corev1.Service)
	if !ok {
		return obj, fmt.Errorf("bad object: got %T, want *corev1.Service", obj)
	}

	t, ok := tmpl.(*corev1.Service)
	if !ok {
		return obj, fmt.Errorf("bad template: got %T, want *corev1.Service", tmpl)
	}

	o.ObjectMeta.Annotations = t.ObjectMeta.Annotations
	o.ObjectMeta.Labels = t.ObjectMeta.Labels
	o.ObjectMeta.OwnerReferences = t.ObjectMeta.OwnerReferences
	o.Spec.Selector = t.Spec.Selector
	o.Spec.Type = t.Spec.Type
	o.Spec.Ports = t.Spec.Ports

	return o, nil
}
