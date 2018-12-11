package strategy

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func deepCopyMapStringString(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	c := map[string]string{}
	for k, v := range m {
		c[k] = v
	}
	return c
}

func Metadata(oldmeta, newmeta *metav1.ObjectMeta) bool {
	changed := false
	if oldmeta.Name != newmeta.Name {
		oldmeta.Name = newmeta.Name
		changed = true
	}
	if oldmeta.Namespace != newmeta.Namespace {
		oldmeta.Namespace = newmeta.Namespace
		changed = true
	}
	if !reflect.DeepEqual(oldmeta.Annotations, newmeta.Annotations) {
		oldmeta.Annotations = deepCopyMapStringString(newmeta.Annotations)
		changed = true
	}
	if !reflect.DeepEqual(oldmeta.Labels, newmeta.Labels) {
		oldmeta.Labels = deepCopyMapStringString(newmeta.Labels)
		changed = true
	}
	if !reflect.DeepEqual(oldmeta.OwnerReferences, newmeta.OwnerReferences) {
		oldmeta.OwnerReferences = make([]metav1.OwnerReference, len(newmeta.OwnerReferences))
		copy(oldmeta.OwnerReferences, newmeta.OwnerReferences)
		changed = true
	}
	return changed
}
