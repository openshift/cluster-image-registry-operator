package migration

import (
	"testing"

	coreapi "k8s.io/api/core/v1"
)

func TestGetVolumeMount(t *testing.T) {
	testCases := []struct {
		mountPath string
		subPath   string
		filename  string
		key       string
	}{
		{
			mountPath: "/etc/config",
			subPath:   "",
			filename:  "/etc/config/config.yaml",
			key:       "config.yaml",
		},
		{
			mountPath: "/etc/config",
			subPath:   "",
			filename:  "/etc/config/foo/bar/baz/config.yaml",
			key:       "foo/bar/baz/config.yaml",
		},
		{
			mountPath: "/etc/config",
			subPath:   "foo",
			filename:  "/etc/config/bar/baz/config.yaml",
			key:       "foo/bar/baz/config.yaml",
		},
		{
			mountPath: "/etc/config",
			subPath:   "foo/bar",
			filename:  "/etc/config/baz/config.yaml",
			key:       "foo/bar/baz/config.yaml",
		},
		{
			mountPath: "/etc/config/config.yaml",
			subPath:   "foo/bar/baz/config.yaml",
			filename:  "/etc/config/config.yaml",
			key:       "foo/bar/baz/config.yaml",
		},
	}
	for _, testCase := range testCases {
		_, key, err := getVolumeMount([]coreapi.VolumeMount{
			{
				Name:      "test-volume",
				MountPath: testCase.mountPath,
				SubPath:   testCase.subPath,
			},
		}, testCase.filename)
		if err != nil {
			t.Errorf("test case %+v: %s", testCase, err)
		}
		if key != testCase.key {
			t.Errorf("test case %+v: got %q, want %q", testCase, key, testCase.key)
		}
	}
}
