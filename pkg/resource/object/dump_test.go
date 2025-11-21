package object

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDumpString(t *testing.T) {
	testcases := []struct {
		object interface{}
		result string
	}{
		{
			object: &corev1.Pod{
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
			result: "metadata.name=\"test-pod\", spec.containers.0.image=\"test-image:v1\", spec.containers.0.name=\"container\"",
		},
		{
			object: &corev1.ConfigMap{
				Data: map[string]string{
					"foo": "aaa",
				},
			},
			result: "data.foo=\"aaa\"",
		},
		{
			object: &corev1.Secret{
				Data: map[string][]byte{
					"foo": []byte("aaa"),
					"bar": []byte("bbb"),
					"xxx": []byte(""),
				},
				StringData: map[string]string{
					"write": "test",
				},
				Type: "Opaque",
			},
			result: "data.bar=<REDACTED>, data.foo=<REDACTED>, data.xxx=\"\", stringData.write=<REDACTED>, type=\"Opaque\"",
		},
	}
	for _, tc := range testcases {
		s, err := DumpString(tc.object)
		if err != nil {
			t.Errorf("DumpString(%v): got an error: %v", tc.object, err)
		}
		if s != tc.result {
			t.Errorf("DumpString(%v): got %q, want %q", tc.object, s, tc.result)
		}
	}
}
