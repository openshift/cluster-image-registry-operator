package util

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
)

func ObjectInfo(o interface{}) string {
	object := o.(metav1.Object)
	s := fmt.Sprintf("%T, ", o)
	if namespace := object.GetNamespace(); namespace != "" {
		s += fmt.Sprintf("Namespace=%s, ", namespace)
	}
	s += fmt.Sprintf("Name=%s", object.GetName())
	return s
}

// AddOwnerRefToObject appends the desired OwnerReference to the object
func AddOwnerRefToObject(obj metav1.Object, ownerRef metav1.OwnerReference) {
	obj.SetOwnerReferences(append(obj.GetOwnerReferences(), ownerRef))
}

// AsOwner returns an OwnerReference set as the CR
func AsOwner(cr *v1.Config) metav1.OwnerReference {
	blockOwnerDeletion := true
	isController := true
	return metav1.OwnerReference{
		APIVersion:         v1.SchemeGroupVersion.String(),
		Kind:               "Config",
		Name:               cr.GetName(),
		UID:                cr.GetUID(),
		BlockOwnerDeletion: &blockOwnerDeletion,
		Controller:         &isController,
	}
}
