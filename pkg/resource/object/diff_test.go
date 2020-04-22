package object

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDiffObject(t *testing.T) {
	testcases := []struct {
		oldObject, newObject interface{}
		diff                 string
	}{
		{
			oldObject: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "test-image:v1",
						},
					},
				},
			},
			newObject: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "test-image:v2",
						},
					},
				},
			},
			diff: "changed:spec.containers.0.image={\"test-image:v1\" -> \"test-image:v2\"}",
		},
		{
			oldObject: &corev1.ConfigMap{
				Data: map[string]string{
					"foo": "aaa",
				},
			},
			newObject: &corev1.ConfigMap{
				Data: map[string]string{
					"bar": "bbb",
				},
			},
			diff: "added:data.bar=\"bbb\", removed:data.foo=\"aaa\"",
		},
		{
			oldObject: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secret",
					Annotations: map[string]string{
						"openshift.io/version": "1.1",
					},
				},
				Data: map[string][]byte{
					"foo": []byte("aaa"),
					"xxx": []byte("ccc"),
					"yyy": []byte("eee"),
				},
			},
			newObject: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "secret",
					Annotations: map[string]string{
						"openshift.io/version": "1.2",
					},
				},
				Data: map[string][]byte{
					"bar": []byte("bbb"),
					"xxx": []byte("ddd"),
					"yyy": []byte(""),
				},
			},
			diff: "added:data.bar=<REDACTED>, removed:data.foo=<REDACTED>, changed:data.xxx={<REDACTED> -> <REDACTED>}, changed:data.yyy={<REDACTED> -> \"\"}, changed:metadata.annotations.openshift.io/version={\"1.1\" -> \"1.2\"}",
		},
	}
	for _, tc := range testcases {
		diff, err := DiffString(tc.oldObject, tc.newObject)
		if err != nil {
			t.Errorf("DiffString(%v, %v): got an error: %v", tc.oldObject, tc.newObject, err)
		}
		if diff != tc.diff {
			t.Errorf("DiffString(%v, %v): got %q, want %q", tc.oldObject, tc.newObject, diff, tc.diff)
		}
	}
}
