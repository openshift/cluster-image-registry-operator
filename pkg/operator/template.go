package operator

import (
	"fmt"

	kmeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cluster-image-registry-operator/pkg/operator/strategy"
)

type Template struct {
	Object   runtime.Object
	Strategy strategy.Strategy
}

func (t *Template) Name() string {
	gvk := t.Object.GetObjectKind().GroupVersionKind()

	var name string
	accessor, err := kmeta.Accessor(t.Object)
	if err != nil {
		name = fmt.Sprintf("%#+v", t.Object)
	} else {
		if namespace := accessor.GetNamespace(); namespace != "" {
			name = fmt.Sprintf("Namespace=%s, ", namespace)
		}
		name += fmt.Sprintf("Name=%s", accessor.GetName())
	}

	return fmt.Sprintf("%s, %s", gvk, name)
}

func (t *Template) Expected() runtime.Object {
	return t.Object.DeepCopyObject()
}

func (t *Template) Apply(o runtime.Object) (runtime.Object, error) {
	return t.Strategy.Apply(o, t.Object)
}
