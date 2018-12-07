package strategy

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func Override(o, n runtime.Object) (bool, error) {
	oval := reflect.Indirect(reflect.ValueOf(o))
	nval := reflect.Indirect(reflect.ValueOf(n))

	typ := oval.Type()
	if typ != nval.Type() {
		return false, fmt.Errorf("cannot override object: old %T, new %T", o, n)
	}

	dgst, err := Checksum(n)
	if err != nil {
		return false, err
	}

	ometa, err := meta.Accessor(o)
	if err != nil {
		return false, fmt.Errorf("unable to get meta accessor for old object: %s", err)
	}
	if ometa.GetAnnotations()[parameters.ChecksumOperatorAnnotation] == dgst {
		return false, nil
	}

	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Name == "Status" {
			continue
		}
		oldfield := oval.Field(i).Addr().Interface()
		newfield := nval.Field(i).Addr().Interface()
		if f.Name == "ObjectMeta" {
			Metadata(oldfield.(*metav1.ObjectMeta), newfield.(*metav1.ObjectMeta))
			continue
		}
		oval.Field(i).Set(nval.Field(i))
	}

	if ometa.GetAnnotations() == nil {
		ometa.SetAnnotations(map[string]string{})
	}
	ometa.GetAnnotations()[parameters.ChecksumOperatorAnnotation] = dgst

	return true, nil
}
