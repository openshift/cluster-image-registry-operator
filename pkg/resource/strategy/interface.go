package strategy

import "k8s.io/apimachinery/pkg/runtime"

type Strategy interface {
	Apply(obj, tmpl runtime.Object) (runtime.Object, error)
}
