package resource

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
)

func TestGetKeepYoungerThan(t *testing.T) {
	duration10s := 10 * time.Second
	duration1h := time.Hour

	testCases := []struct {
		imagePruner *imageregistryv1.ImagePruner
		want        string
	}{
		{
			imagePruner: &imageregistryv1.ImagePruner{
				Spec: imageregistryv1.ImagePrunerSpec{},
			},
			want: "60m",
		},
		{
			imagePruner: &imageregistryv1.ImagePruner{
				Spec: imageregistryv1.ImagePrunerSpec{
					KeepYoungerThan: &duration10s,
				},
			},
			want: "10s",
		},
		{
			imagePruner: &imageregistryv1.ImagePruner{
				Spec: imageregistryv1.ImagePrunerSpec{
					KeepYoungerThanDuration: &metav1.Duration{
						Duration: duration1h,
					},
				},
			},
			want: "1h0m0s",
		},
		{
			imagePruner: &imageregistryv1.ImagePruner{
				Spec: imageregistryv1.ImagePrunerSpec{
					KeepYoungerThan: &duration1h,
					KeepYoungerThanDuration: &metav1.Duration{
						Duration: duration10s,
					},
				},
			},
			want: "10s",
		},
	}
	for _, tc := range testCases {
		g := generatorPrunerCronJob{}
		got := g.getKeepYoungerThan(tc.imagePruner)
		if got != tc.want {
			t.Errorf("got %v, want %v (%#+v)", got, tc.want, tc.imagePruner)
		}
	}
}

func TestLogLevel(t *testing.T) {
	testCases := []struct {
		imagePruner *imageregistryv1.ImagePruner
		want        int
	}{
		{
			imagePruner: &imageregistryv1.ImagePruner{
				Spec: imageregistryv1.ImagePrunerSpec{},
			},
			want: 1,
		},
		{
			imagePruner: &imageregistryv1.ImagePruner{
				Spec: imageregistryv1.ImagePrunerSpec{
					LogLevel: "Debug",
				},
			},
			want: 4,
		},
	}
	for _, tc := range testCases {
		g := generatorPrunerCronJob{}
		got := g.getLogLevel(tc.imagePruner)
		if got != tc.want {
			t.Errorf("got %v, want %v (%#+v)", got, tc.want, tc.imagePruner)
		}
	}
}
