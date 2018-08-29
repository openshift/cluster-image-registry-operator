package strategy

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type ConfigMap struct{}

var _ Strategy = ConfigMap{}

func (cm ConfigMap) Apply(obj, tmpl runtime.Object) (runtime.Object, error) {
	o, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return obj, fmt.Errorf("bad object: got %T, want *corev1.ConfigMap", obj)
	}

	t, ok := tmpl.(*corev1.ConfigMap)
	if !ok {
		return obj, fmt.Errorf("bad template: got %T, want *corev1.ConfigMap", tmpl)
	}

	o.ObjectMeta.Annotations = t.ObjectMeta.Annotations
	o.ObjectMeta.Labels = t.ObjectMeta.Labels
	o.ObjectMeta.OwnerReferences = t.ObjectMeta.OwnerReferences

	if t.Data != nil {
		if o.Data == nil {
			o.Data = make(map[string]string)
		}
		for name, value := range t.Data {
			o.Data[name] = value
		}
	}

	if t.BinaryData != nil {
		if o.BinaryData == nil {
			o.BinaryData = make(map[string][]byte)
		}
		for name, value := range t.BinaryData {
			o.BinaryData[name] = value
		}
	}

	return o, nil
}
