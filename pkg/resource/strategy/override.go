package strategy

import (
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

type Override struct{}

var _ Strategy = Override{}

func (s Override) Apply(obj, tmpl runtime.Object) (runtime.Object, error) {
	accessor := kmeta.NewAccessor()

	rv, err := accessor.ResourceVersion(obj)
	if err != nil {
		return obj, err
	}

	result := tmpl.DeepCopyObject()

	err = accessor.SetResourceVersion(result, rv)
	if err != nil {
		return obj, err
	}

	return result, nil
}
