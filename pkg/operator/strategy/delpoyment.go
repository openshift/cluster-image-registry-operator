package strategy

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	appsv1 "k8s.io/api/apps/v1"
)

type Deployment struct{}

var _ Strategy = Deployment{}

func (s Deployment) Apply(obj, tmpl runtime.Object) (runtime.Object, error) {
	o, ok := obj.(*appsv1.Deployment)
	if !ok {
		return obj, fmt.Errorf("bad object: got %T, want *appsv1.Deployment", obj)
	}

	t, ok := tmpl.(*appsv1.Deployment)
	if !ok {
		return obj, fmt.Errorf("bad template: got %T, want *appsv1.Deployment", tmpl)
	}

	o.ObjectMeta.Annotations = t.ObjectMeta.Annotations
	o.ObjectMeta.Labels = t.ObjectMeta.Labels
	o.ObjectMeta.OwnerReferences = t.ObjectMeta.OwnerReferences
	o.Spec = t.Spec

	return o, nil
}
