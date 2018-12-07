package resource

import (
	"fmt"

	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Getter interface {
	Type() interface{}
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
