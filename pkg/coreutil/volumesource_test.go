package coreutil

import (
	"testing"

	coreapi "k8s.io/api/core/v1"
)

func TestGetVolumeSourceField(t *testing.T) {
	field, err := GetVolumeSourceField(coreapi.VolumeSource{
		Secret: &coreapi.SecretVolumeSource{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if field.Name != "Secret" {
		t.Fatalf("got %q, want %q", field.Name, "Secret")
	}

	field, err = GetVolumeSourceField(coreapi.VolumeSource{
		Secret:    &coreapi.SecretVolumeSource{},
		ConfigMap: &coreapi.ConfigMapVolumeSource{},
	})
	if err.Error() != "too many sources for the volume found: Secret, ConfigMap" {
		t.Fatalf("unexpected error: %s", err)
	}
}
