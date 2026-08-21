package operator

import (
	"testing"

	configapi "github.com/openshift/api/config/v1"
)

func TestImageStreamImportMode(t *testing.T) {
	tests := []struct {
		name         string
		specMode     configapi.ImportModeType
		currentMode  configapi.ImportModeType
		architecture configapi.ClusterVersionArchitecture
		want         configapi.ImportModeType
	}{
		{
			name:         "spec mode overrides multi-arch cluster",
			specMode:     configapi.ImportModeLegacy,
			architecture: configapi.ClusterVersionArchitectureMulti,
			want:         configapi.ImportModeLegacy,
		},
		{
			name:         "spec mode overrides single-arch cluster",
			specMode:     configapi.ImportModePreserveOriginal,
			architecture: "amd64",
			want:         configapi.ImportModePreserveOriginal,
		},
		{
			name:         "multi-arch cluster returns PreserveOriginal",
			architecture: configapi.ClusterVersionArchitectureMulti,
			want:         configapi.ImportModePreserveOriginal,
		},
		{
			name:         "single-arch cluster returns Legacy",
			architecture: "amd64",
			want:         configapi.ImportModeLegacy,
		},
		{
			name:         "empty architecture preserves existing PreserveOriginal",
			currentMode:  configapi.ImportModePreserveOriginal,
			architecture: "",
			want:         configapi.ImportModePreserveOriginal,
		},
		{
			name:         "empty architecture preserves existing Legacy",
			currentMode:  configapi.ImportModeLegacy,
			architecture: "",
			want:         configapi.ImportModeLegacy,
		},
		{
			name:         "empty architecture with no existing mode stays empty",
			currentMode:  "",
			architecture: "",
			want:         "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := imageStreamImportMode(tc.specMode, tc.currentMode, tc.architecture)
			if got != tc.want {
				t.Errorf("imageStreamImportMode(%q, %q, %q) = %q, want %q",
					tc.specMode, tc.currentMode, tc.architecture, got, tc.want)
			}
		})
	}
}
