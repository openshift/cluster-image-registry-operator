package metrics

import (
	"crypto/tls"
	"net/http"
	"os"
	"testing"
	"time"

	prometheusutil "github.com/openshift/cluster-image-registry-operator/test/util/prometheus"
)

func TestMain(m *testing.M) {
	// sets the default http client to skip certificate check.
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	ch := make(chan struct{})
	tlsKey, tlsCRT, err := StartTestMetricsServer(5000, ch)
	if err != nil {
		panic(err)
	}

	// give http handlers/server some time to process certificates and
	// get online before running tests.
	time.Sleep(time.Second)

	code := m.Run()
	os.Remove(tlsKey)
	os.Remove(tlsCRT)
	close(ch)
	os.Exit(code)
}

func TestRun(t *testing.T) {
	resp, err := http.Get("https://localhost:5000/metrics")
	if err != nil {
		t.Fatalf("error requesting metrics server: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, received %d instead.", resp.StatusCode)
	}
}

func TestStorageReconfigured(t *testing.T) {
	metricName := "image_registry_operator_storage_reconfigured_total"
	for _, tt := range []struct {
		name string
		iter int
		expt float64
	}{
		{
			name: "zeroed",
			iter: 0,
			expt: 0,
		},
		{
			name: "increase to five",
			iter: 5,
			expt: 5,
		},
		{
			name: "increase to ten",
			iter: 5,
			expt: 10,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.iter; i++ {
				StorageReconfigured()
			}

			metrics, err := prometheusutil.GetMetricsWithName("https://localhost:5000/metrics", metricName)
			if err != nil {
				t.Fatalf("error locating metric %s: %v", metricName, err)
			}
			if len(metrics) == 0 {
				t.Fatalf("unable to locate metric %s", metricName)
			}

			val := *metrics[0].Counter.Value
			if val != tt.expt {
				t.Errorf("expected %.0f, found %.0f", tt.expt, val)
			}
		})
	}
}

func TestImagePrunerInstallStatus(t *testing.T) {
	metricName := "image_registry_operator_image_pruner_install_status"
	testCases := []struct {
		name      string
		installed bool
		enabled   bool
	}{
		{
			name:      "not installed",
			installed: false,
			enabled:   false,
		},
		{
			name:      "suspended",
			installed: true,
			enabled:   false,
		},
		{
			name:      "enabled",
			installed: true,
			enabled:   true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ImagePrunerInstallStatus(tc.installed, tc.enabled)

			metrics, err := prometheusutil.GetMetricsWithName("https://localhost:5000/metrics", metricName)
			if err != nil {
				t.Fatalf("error locating metric: %v", err)
			}

			if len(metrics) == 0 {
				t.Fatalf("unable to locate metric %s", metricName)
			}

			for _, m := range metrics {
				if !tc.installed && m.Gauge.GetValue() != 0 {
					t.Errorf("expected metric %s to be 0, got %f", metricName, m.Gauge.GetValue())
				}
				if tc.installed && !tc.enabled && m.Gauge.GetValue() != 1 {
					t.Errorf("expected metric %s to be 1, got %f", metricName, m.Gauge.GetValue())
				}
				if tc.installed && tc.enabled && m.Gauge.GetValue() != 2 {
					t.Errorf("expected metric %s to be 2, got %f", metricName, m.Gauge.GetValue())
				}
			}

		})
	}

}
