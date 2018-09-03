package strategy

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Secret struct{}

var _ Strategy = Secret{}

func (cm Secret) Apply(obj, tmpl runtime.Object) (runtime.Object, error) {
	o, ok := obj.(*corev1.Secret)
	if !ok {
		return obj, fmt.Errorf("bad object: got %T, want *corev1.Secret", obj)
	}

	t, ok := tmpl.(*corev1.Secret)
	if !ok {
		return obj, fmt.Errorf("bad template: got %T, want *corev1.Secret", tmpl)
	}

	o.ObjectMeta.Annotations = t.ObjectMeta.Annotations
	o.ObjectMeta.Labels = t.ObjectMeta.Labels
	o.ObjectMeta.OwnerReferences = t.ObjectMeta.OwnerReferences

	if t.Data != nil {
		if o.Data == nil {
			o.Data = make(map[string][]byte)
		}
		for name, value := range t.Data {
			o.Data[name] = value
		}
	}

	if t.StringData != nil {
		if o.StringData == nil {
			o.StringData = make(map[string]string)
		}
		for name, value := range t.StringData {
			o.StringData[name] = value
		}
	}

	return o, nil
}
