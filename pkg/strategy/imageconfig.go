package strategy

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	configv1 "github.com/openshift/api/config/v1"
)

type ImageConfig struct{}

var _ Strategy = ImageConfig{}

func (s ImageConfig) Apply(obj, tmpl runtime.Object) (runtime.Object, error) {
	o, ok := obj.(*configv1.Image)
	if !ok {
		return obj, fmt.Errorf("bad object: got %T, want *configv1.Image", obj)
	}

	t, ok := tmpl.(*configv1.Image)
	if !ok {
		return obj, fmt.Errorf("bad template: got %T, want *configv1.Image", tmpl)
	}

	o.Status.InternalRegistryHostname = t.Status.InternalRegistryHostname
	return o, nil
}
