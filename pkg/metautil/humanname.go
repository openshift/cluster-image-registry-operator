package metautil

import (
	"fmt"

	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type MetaTypeObject interface {
	schema.ObjectKind
	metaapi.Object
}

func TypeAndName(o MetaTypeObject) string {
	namespace := o.GetNamespace()
	if namespace != "" {
		namespace += "/"
	}
	return fmt.Sprintf("%s %s%s", o.GroupVersionKind().Kind, namespace, o.GetName())
}
