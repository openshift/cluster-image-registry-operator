package strategy

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

func TestOverride(t *testing.T) {
	o := &imageregistryapi.ImageRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "foo",
			ResourceVersion: "12345",
			Annotations: map[string]string{
				"hello": "world",
			},
		},
		Spec: imageregistryapi.ImageRegistrySpec{
			HTTPSecret: "secret",
		},
	}
	n := &imageregistryapi.ImageRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
			Annotations: map[string]string{
				"foo": "bar",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "owner-name",
				},
			},
		},
		Spec: imageregistryapi.ImageRegistrySpec{
			HTTPSecret: "new-secret",
		},
	}
	changed, err := Override(o, n)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("object is expected to be changed, but changed=false")
	}
	if o.ResourceVersion != "12345" {
		t.Errorf("resouce version is changed: %v", o.ResourceVersion)
	}
	if val, ok := o.Annotations["hello"]; ok {
		t.Errorf("annotation hello: expected to be removed, got %q", val)
	}
	if val := o.Annotations["foo"]; val != "bar" {
		t.Errorf("annotation foo: got %q, want %q", val, "bar")
	}
	if len(o.OwnerReferences) != 1 || o.OwnerReferences[0].Name != "owner-name" {
		t.Errorf("unexpected owner references: %#v", o.OwnerReferences)
	}
	if val := o.Spec.HTTPSecret; val != "new-secret" {
		t.Errorf("httpsecret: got %q, want %q", val, "new-secret")
	}
	changed, err = Override(o, n)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("the second call is expected to do nothing")
	}
}
