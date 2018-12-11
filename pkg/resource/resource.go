package resource

import (
	"fmt"

	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
)

type Getter interface {
	Type() runtime.Object
	GetName() string
	GetNamespace() string
	Get() (runtime.Object, error)
}

type Mutator interface {
	Getter
	Create() error
	Update(o runtime.Object) (bool, error)
	Delete(opts *metaapi.DeleteOptions) error
}

func Name(o Getter) string {
	name := fmt.Sprintf("%T, ", o.Type())

	if namespace := o.GetNamespace(); namespace != "" {
		name += fmt.Sprintf("Namespace=%s, ", namespace)
	}

	name += fmt.Sprintf("Name=%s", o.GetName())

	return name
}

type expecter interface {
	Type() runtime.Object
	expected() (runtime.Object, error)
}

func commonCreate(gen expecter, create func(obj runtime.Object) (runtime.Object, error)) error {
	o := gen.Type()

	n, err := gen.expected()
	if err != nil {
		return err
	}

	_, err = strategy.Override(o, n)
	if err != nil {
		return err
	}

	_, err = create(o)
	return err
}

func commonUpdate(gen expecter, o runtime.Object, update func(obj runtime.Object) (runtime.Object, error)) (bool, error) {
	n, err := gen.expected()
	if err != nil {
		return false, err
	}

	updated, err := strategy.Override(o, n)
	if !updated || err != nil {
		return updated, err
	}

	_, err = update(o)
	return true, err
}
