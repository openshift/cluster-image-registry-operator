package envvar

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestEnvVars(t *testing.T) {
	l := List{
		{Name: "INT", Value: 10},
		{Name: "BOOL", Value: true},
		{Name: "STRING", Value: "foo"},
		{Name: "COMPLEX_STRING", Value: "# foo'bar\"baz"},
		{Name: "NUMERIC_STRING", Value: "10"},
		{Name: "BOOL_STRING", Value: "true"},
		{Name: "SECRET", Value: "password", Secret: true},
	}
	expected := []corev1.EnvVar{
		{Name: "INT", Value: "10"},
		{Name: "BOOL", Value: "true"},
		{Name: "STRING", Value: "foo"},
		{Name: "COMPLEX_STRING", Value: "'# foo''bar\"baz'"},
		{Name: "NUMERIC_STRING", Value: "\"10\""},
		{Name: "BOOL_STRING", Value: "\"true\""},
		{
			Name: "SECRET",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "my-secret",
					},
					Key: "SECRET",
				},
			},
		},
	}

	envvars, err := l.EnvVars("my-secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(envvars) != len(expected) {
		t.Fatalf("got %d elements, want %d: %#+v", len(envvars), len(l), envvars)
	}
	for i, envvar := range envvars {
		if !reflect.DeepEqual(envvar, expected[i]) {
			t.Errorf("envvar %s: got %#+v, want %#+v", l[i].Name, envvar, expected[i])
		}
	}
}

func TestSecretData(t *testing.T) {
	l := List{
		{Name: "STRING", Value: "foo"},
		{Name: "SECRET", Value: "password", Secret: true},
		{Name: "COMPLEX_SECRET", Value: "# foo'bar\"baz", Secret: true},
	}
	expected := map[string]string{
		"SECRET":         "password",
		"COMPLEX_SECRET": "'# foo''bar\"baz'",
	}

	data, err := l.SecretData()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data, expected) {
		t.Errorf("got %#+v, want %#+v", data, expected)
	}
}
