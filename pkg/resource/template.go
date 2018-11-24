package resource

import (
	"fmt"

	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

type Template struct {
	Object      runtime.Object
	Annotations map[string]string
	Strategy    strategy.Strategy

	Get    func() (runtime.Object, error)
	Create func() error
	Update func(runtime.Object) error
	Delete func(*metav1.DeleteOptions) error
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
