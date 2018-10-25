package strategy

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	appsv1 "github.com/openshift/api/apps/v1"
)

type DeploymentConfig struct{}

var _ Strategy = DeploymentConfig{}

func (s DeploymentConfig) Apply(obj, tmpl runtime.Object) (runtime.Object, error) {
	o, ok := obj.(*appsv1.DeploymentConfig)
	if !ok {
		return obj, fmt.Errorf("bad object: got %T, want *appsv1.DeploymentConfig", obj)
	}

	t, ok := tmpl.(*appsv1.DeploymentConfig)
	if !ok {
		return obj, fmt.Errorf("bad template: got %T, want *appsv1.DeploymentConfig", tmpl)
	}

	o.ObjectMeta.Annotations = t.ObjectMeta.Annotations
	o.ObjectMeta.Labels = t.ObjectMeta.Labels
	o.ObjectMeta.OwnerReferences = t.ObjectMeta.OwnerReferences
	o.Spec = t.Spec

	return o, nil
}
